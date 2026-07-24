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

	"beacon/internal/awg"
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
	case "enable":
		setEnabled(os.Args[2:], true)
	case "disable":
		setEnabled(os.Args[2:], false)
	case "rename":
		renameUser(os.Args[2:])
	case "https":
		setupHTTPS(os.Args[2:])
	case "protocol":
		protocolCmd(os.Args[2:])
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
  beacon enable <id>     включить ключ
  beacon disable <id>    выключить ключ (без удаления)
  beacon rename <id> ..  переименовать ключ
  beacon https [домен]   HTTPS панели через Let's Encrypt (авто <ip>.sslip.io)
  beacon protocol [реж]  переключить протокол: reality | amneziawg (без аргумента — показать текущий)
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
	aw := awg.New(cfg, st, paths.AWGConfig)

	srv := server.New(cfg, paths, st, xr, aw, version)
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

	// ключи и параметры обфускации AmneziaWG генерируем сразу, чтобы переключение протокола было мгновенным
	awgPriv, awgPub, err := awg.GenerateKeypair()
	fatal(err)
	cfg.AWGPrivateKey = awgPriv
	cfg.AWGPublicKey = awgPub
	p := awg.GenerateParams()
	cfg.AWGJc, cfg.AWGJmin, cfg.AWGJmax = p.Jc, p.Jmin, p.Jmax
	cfg.AWGS1, cfg.AWGS2 = p.S1, p.S2
	cfg.AWGH1, cfg.AWGH2, cfg.AWGH3, cfg.AWGH4 = p.H1, p.H2, p.H3, p.H4

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
	fmt.Printf("пароль: %s\n", pw)
	fmt.Printf("панель: %s\n", panelURL(cfg))
	fmt.Println("применится после: systemctl restart beacon")
}

// setupHTTPS включает Let's Encrypt для домена (по умолчанию авто <ip>.sslip.io).
func setupHTTPS(args []string) {
	paths := config.DefaultPaths()
	cfg, err := config.Load(paths.ConfigFile)
	fatal(err)

	domain := ""
	if len(args) > 0 {
		domain = args[0]
	} else if isIPv4(cfg.PublicHost) {
		domain = strings.ReplaceAll(cfg.PublicHost, ".", "-") + ".sslip.io"
	}
	if domain == "" {
		log.Fatal("укажи домен: beacon https <домен>")
	}
	cfg.SetPath(paths.ConfigFile)
	cfg.ACMEDomain = domain
	fatal(cfg.Save())

	fmt.Printf("HTTPS включён: https://%s%s\n", domain, cfg.ListenAddr)
	fmt.Println("Нужен свободный порт 80 и открытые 80/443 в фаерволе.")
	fmt.Println("Перезапусти: systemctl restart beacon")
}

// protocolCmd переключает активный VPN-протокол (reality/amneziawg) или печатает текущий без аргумента.
func protocolCmd(args []string) {
	paths := config.DefaultPaths()
	cfg, err := config.Load(paths.ConfigFile)
	fatal(err)
	if len(args) == 0 {
		fmt.Println(cfg.Protocol)
		return
	}
	p := args[0]
	if p != "reality" && p != "amneziawg" {
		log.Fatal("протокол должен быть reality или amneziawg")
	}
	if p == cfg.Protocol {
		fmt.Printf("уже активен: %s\n", p)
		return
	}
	cfg.SetPath(paths.ConfigFile)
	cfg.Protocol = p
	fatal(cfg.Save())

	st, err := store.Open(paths.DataFile)
	fatal(err)
	if p == "amneziawg" {
		aw := awg.New(cfg, st, paths.AWGConfig)
		if err := aw.Apply(); err != nil {
			log.Printf("предупреждение: AmneziaWG не поднялся (%v) — проверь, установлен ли awg-quick", err)
		}
	} else {
		aw := awg.New(cfg, st, paths.AWGConfig)
		_ = aw.Down()
		xr := xray.New(cfg, st, paths.XrayConfig, "xray")
		if err := xr.Apply(); err != nil {
			log.Printf("предупреждение: перезапуск Xray: %v", err)
		}
	}
	fmt.Printf("протокол переключён: %s\n", p)
	fmt.Println("перезапусти панель: systemctl restart beacon")
}

func isIPv4(s string) bool {
	ip := net.ParseIP(s)
	return ip != nil && ip.To4() != nil
}

func setEnabled(args []string, on bool) {
	if len(args) == 0 {
		log.Fatal("укажи id: beacon enable|disable <id>")
	}
	id := args[0]
	paths := config.DefaultPaths()
	cfg, err := config.Load(paths.ConfigFile)
	fatal(err)
	st, err := store.Open(paths.DataFile)
	fatal(err)
	_, changed, err := st.Update(id, nil, nil, &on)
	fatal(err)
	if changed {
		xr := xray.New(cfg, st, paths.XrayConfig, "xray")
		if err := xr.Apply(); err != nil {
			log.Printf("предупреждение: перезапуск Xray: %v", err)
		}
	}
	state := "выключен"
	if on {
		state = "включён"
	}
	fmt.Printf("ключ %s %s\n", id, state)
}

func renameUser(args []string) {
	fs := flag.NewFlagSet("rename", flag.ExitOnError)
	device := fs.String("device", "", "новое устройство/заметка (опционально)")
	_ = fs.Parse(args)
	rest := fs.Args()
	if len(rest) < 2 {
		log.Fatal("использование: beacon rename [--device X] <id> <новое имя>")
	}
	id := rest[0]
	name := strings.Join(rest[1:], " ")

	paths := config.DefaultPaths()
	st, err := store.Open(paths.DataFile)
	fatal(err)
	var dev *string
	if *device != "" {
		dev = device
	}
	u, _, err := st.Update(id, &name, dev, nil)
	fatal(err)
	fmt.Printf("переименован: %s → %s\n", id, u.Name)
}

// ---- helpers ----

func printSummary(cfg *config.Config, pw, link string) {
	fmt.Println()
	fmt.Printf("панель:  %s\n", panelURL(cfg))
	fmt.Printf("пароль:  %s\n", pw)
	fmt.Printf("ключ:    %s\n", link)
	fmt.Println()
	if a, err := qr.ASCII(link); err == nil {
		fmt.Println(a)
	}
}

// panelURL собирает URL панели из публичного хоста и адреса прослушивания.
func panelURL(cfg *config.Config) string {
	host := cfg.PublicHost
	if cfg.ACMEDomain != "" {
		host = cfg.ACMEDomain
	}
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
