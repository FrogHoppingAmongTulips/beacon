// Команда beacon: веб-панель и CLI для self-hosted VPN (VLESS + Reality).
//
// Использование:
//
//	beacon                 запустить панель (по умолчанию)
//	beacon serve           то же самое
//	beacon setup [флаги]   первичная настройка: ключи, пароль, первый пользователь
//	beacon add-user <имя>  создать ключ и напечатать ссылку + QR
//	beacon list            список пользователей
//	beacon reset-password  сменить пароль панели
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"beacon/internal/config"
	"beacon/internal/qr"
	"beacon/internal/server"
	"beacon/internal/store"
	"beacon/internal/vpn"
	"beacon/internal/xray"
)

// version подставляется при сборке через -ldflags "-X main.version=...".
var version = "dev"

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		serve()
		return
	}
	switch os.Args[1] {
	case "serve":
		serve()
	case "setup":
		setup(os.Args[2:])
	case "add-user":
		addUser(os.Args[2:])
	case "list":
		listUsers()
	case "reset-password":
		resetPassword(os.Args[2:])
	case "version", "-v", "--version":
		fmt.Println(version)
	case "-h", "--help", "help":
		usage()
	default:
		// возможно это флаги для serve
		if strings.HasPrefix(os.Args[1], "-") {
			serve()
			return
		}
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Print(`beacon — панель self-hosted VPN (VLESS + Reality)

  beacon                 запустить веб-панель
  beacon setup [флаги]   первичная настройка
  beacon add-user <имя>  создать ключ (печатает ссылку и QR)
  beacon list            список пользователей
  beacon reset-password  сменить пароль панели
  beacon version         версия сборки

setup флаги: --host, --listen, --port, --sni, --password, --user, --force
`)
}

// serve запускает веб-панель.
func serve() {
	paths := config.DefaultPaths()
	cfg, err := config.Load(paths.ConfigFile)
	if err != nil {
		log.Fatalf("нет конфига %s — сначала выполни `beacon setup` (%v)", paths.ConfigFile, err)
	}
	st, err := store.Open(paths.DataFile)
	fatal(err)

	xr := xray.New(cfg, st, paths.XrayConfig, "xray")
	// синхронизируем конфиг Xray с текущим списком пользователей на старте
	if err := xr.WriteConfig(); err != nil {
		log.Printf("предупреждение: запись конфига Xray: %v", err)
	}

	srv := server.New(cfg, paths, st, xr)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// фоновый опрос статистики трафика Xray (online-статус + байты по каждому)
	go xray.NewStatsPoller(xr).Run(ctx, st, 5*time.Second)

	if err := srv.Run(ctx); err != nil {
		log.Fatal(err)
	}
}

// setup — первичная настройка, вызывается установщиком.
func setup(args []string) {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	host := fs.String("host", "", "публичный IP/домен сервера для ссылок vless")
	listen := fs.String("listen", ":8443", "адрес веб-панели")
	port := fs.Int("port", 443, "порт VPN inbound")
	sni := fs.String("sni", "www.microsoft.com", "маскировочный домен (SNI/dest)")
	pass := fs.String("password", "", "пароль панели (иначе сгенерируется)")
	firstUser := fs.String("user", "Первый ключ", "имя первого пользователя")
	force := fs.Bool("force", false, "перезаписать существующий конфиг")
	_ = fs.Parse(args)

	paths := config.DefaultPaths()
	if _, err := os.Stat(paths.ConfigFile); err == nil && !*force {
		log.Fatalf("конфиг уже существует: %s (перезапись — флаг --force)", paths.ConfigFile)
	}

	cfg := config.NewDefault()
	cfg.SetPath(paths.ConfigFile)
	cfg.ListenAddr = *listen
	cfg.VPNPort = *port
	cfg.SNI = *sni
	cfg.Dest = *sni + ":443"
	cfg.PublicHost = firstNonEmpty(*host, detectHost())

	priv, pub, err := xray.GenerateX25519()
	fatal(err)
	cfg.PrivateKey = priv
	cfg.PublicKey = pub
	cfg.ShortIDs = []string{xray.GenShortID()}

	pw := firstNonEmpty(*pass, config.GenPassword())
	cfg.SetPassword(pw)
	fatal(cfg.Save())

	st, err := store.Open(paths.DataFile)
	fatal(err)
	u, err := st.Add(*firstUser, "первое устройство")
	fatal(err)

	xr := xray.New(cfg, st, paths.XrayConfig, "xray")
	if err := xr.WriteConfig(); err != nil {
		log.Printf("предупреждение: запись конфига Xray: %v", err)
	}

	printSummary(cfg, pw, vpn.Link(cfg, u))
}

func addUser(args []string) {
	fs := flag.NewFlagSet("add-user", flag.ExitOnError)
	device := fs.String("device", "", "устройство/заметка")
	_ = fs.Parse(args)
	name := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if name == "" {
		log.Fatal("укажи имя: beacon add-user <имя>")
	}

	paths := config.DefaultPaths()
	cfg, err := config.Load(paths.ConfigFile)
	fatal(err)
	st, err := store.Open(paths.DataFile)
	fatal(err)
	u, err := st.Add(name, *device)
	fatal(err)

	xr := xray.New(cfg, st, paths.XrayConfig, "xray")
	if err := xr.Apply(); err != nil {
		log.Printf("предупреждение: перезапуск Xray: %v", err)
	}

	link := vpn.Link(cfg, u)
	fmt.Println(link)
	if a, err := qr.ASCII(link); err == nil {
		fmt.Println(a)
	}
}

func listUsers() {
	paths := config.DefaultPaths()
	st, err := store.Open(paths.DataFile)
	fatal(err)
	users := st.List()
	if len(users) == 0 {
		fmt.Println("пользователей нет")
		return
	}
	for _, u := range users {
		status := "offline"
		if u.Online() {
			status = "online"
		}
		fmt.Printf("%-24s %-8s %s\n", u.Name, status, u.ID)
	}
}

func resetPassword(args []string) {
	paths := config.DefaultPaths()
	cfg, err := config.Load(paths.ConfigFile)
	fatal(err)
	pw := config.GenPassword()
	if len(args) > 0 && args[0] != "" {
		pw = args[0]
	}
	cfg.SetPath(paths.ConfigFile)
	cfg.SetPassword(pw)
	fatal(cfg.Save())
	fmt.Printf("Новый пароль панели: %s\n", pw)
}

// ---- helpers ----

func printSummary(cfg *config.Config, pw, link string) {
	fmt.Println()
	fmt.Println("──────────────  beacon установлен  ──────────────")
	fmt.Printf("  Панель:  %s\n", panelURL(cfg))
	fmt.Printf("  Пароль:  %s\n", pw)
	fmt.Println("  (сертификат самоподписанный — браузер предупредит, это ок)")
	fmt.Println()
	fmt.Println("  Первый ключ подключения (vless):")
	fmt.Printf("  %s\n", link)
	fmt.Println()
	if a, err := qr.ASCII(link); err == nil {
		fmt.Println(a)
	}
	fmt.Println("  Отсканируй QR в v2rayNG / Streisand / Hiddify.")
	fmt.Println("─────────────────────────────────────────────────")
}

// panelURL собирает URL панели из публичного хоста и адреса прослушивания.
func panelURL(cfg *config.Config) string {
	host := cfg.PublicHost
	addr := cfg.ListenAddr
	port := strings.TrimPrefix(addr, ":")
	if h, p, err := net.SplitHostPort(addr); err == nil {
		if h != "" {
			host = h
		}
		port = p
	}
	u := url.URL{Scheme: "https", Host: net.JoinHostPort(host, port)}
	return u.String()
}

// detectHost — грубое определение основного IP сервера (для VPS обычно публичный).
func detectHost() string {
	c, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer c.Close()
	if a, ok := c.LocalAddr().(*net.UDPAddr); ok {
		return a.IP.String()
	}
	return "127.0.0.1"
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func fatal(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
