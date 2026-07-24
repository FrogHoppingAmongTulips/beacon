// Package xray собирает config.json для Xray-core (VLESS + Reality)
// из настроек aqu и списка пользователей, перезапускает сервис и читает статистику трафика.
package xray

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"aqu/internal/config"
	"aqu/internal/store"
)

// Локальный gRPC-эндпоинт Stats API внутри Xray (доступен только с 127.0.0.1).
const (
	apiPort = 10085
	apiAddr = "127.0.0.1:10085"
)

// Manager управляет конфигом, процессом и статистикой Xray.
type Manager struct {
	cfg        *config.Config
	store      *store.Store
	configPath string // путь к config.json Xray
	service    string // имя systemd-сервиса Xray
	bin        string // путь к бинарнику xray (для api-команд)
}

// New создаёт менеджер. service обычно "xray". Путь к бинарнику можно переопределить AQU_XRAY_BIN.
func New(cfg *config.Config, st *store.Store, configPath, service string) *Manager {
	if service == "" {
		service = "xray"
	}
	bin := os.Getenv("AQU_XRAY_BIN")
	if bin == "" {
		bin = "xray"
	}
	return &Manager{cfg: cfg, store: st, configPath: configPath, service: service, bin: bin}
}

// Apply перегенерирует config.json и перезапускает Xray.
func (m *Manager) Apply() error {
	if err := m.WriteConfig(); err != nil {
		return err
	}
	return m.Reload()
}

// WriteConfig атомарно пишет config.json для Xray.
func (m *Manager) WriteConfig() error {
	b, err := m.BuildConfig()
	if err != nil {
		return err
	}
	if dir := filepath.Dir(m.configPath); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp := m.configPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, m.configPath)
}

// Reload перезапускает сервис Xray, чтобы подхватить новый конфиг.
// На хосте без systemd (dev) вернёт ошибку — вызывающий код её логирует, но не падает.
func (m *Manager) Reload() error {
	cmd := exec.Command("systemctl", "restart", m.service)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restart %s: %v: %s", m.service, err, out)
	}
	return nil
}

// Traffic — счётчики трафика пользователя (байты, накопительно с последнего старта Xray).
type Traffic struct {
	Up   uint64
	Down uint64
}

// QueryTraffic читает статистику по пользователям через `xray api statsquery`.
// Ключ результата — email клиента (у нас это UUID пользователя).
func (m *Manager) QueryTraffic() (map[string]Traffic, error) {
	out, err := exec.Command(m.bin, "api", "statsquery", "--server="+apiAddr, "-pattern=user>>>").Output()
	if err != nil {
		return nil, err
	}
	return parseStats(out)
}

// parseStats разбирает JSON-вывод `xray api statsquery` в трафик по email пользователя.
func parseStats(out []byte) (map[string]Traffic, error) {
	var reply struct {
		Stat []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"stat"`
	}
	if err := json.Unmarshal(out, &reply); err != nil {
		return nil, err
	}
	res := make(map[string]Traffic)
	for _, s := range reply.Stat {
		// формат имени: user>>>EMAIL>>>traffic>>>uplink|downlink
		parts := strings.Split(s.Name, ">>>")
		if len(parts) != 4 || parts[0] != "user" || parts[2] != "traffic" {
			continue
		}
		email, dir := parts[1], parts[3]
		v, _ := strconv.ParseUint(s.Value, 10, 64)
		t := res[email]
		switch dir {
		case "uplink":
			t.Up = v
		case "downlink":
			t.Down = v
		}
		res[email] = t
	}
	return res, nil
}

// BuildConfig формирует JSON-конфиг Xray из cfg и пользователей store.
func (m *Manager) BuildConfig() ([]byte, error) {
	c := m.cfg
	clients := make([]client, 0)
	for _, u := range m.store.List() {
		if !u.Enabled {
			continue
		}
		clients = append(clients, client{
			ID:    u.ID,
			Email: u.ID, // email = uuid: уникально и удобно матчить статистику
			Flow:  "xtls-rprx-vision",
		})
	}

	shortIDs := c.ShortIDs
	if len(shortIDs) == 0 {
		shortIDs = []string{GenShortID()}
	}

	vless := inbound{
		Tag:      "vless-reality",
		Listen:   "0.0.0.0",
		Port:     c.VPNPort,
		Protocol: "vless",
		Settings: inboundSettings{Clients: clients, Decryption: "none"},
		StreamSettings: streamSettings{
			Network:  "tcp",
			Security: "reality",
			Reality: realitySettings{
				Show:        false,
				Dest:        c.Dest,
				Xver:        0,
				ServerNames: []string{c.SNI},
				PrivateKey:  c.PrivateKey,
				ShortIDs:    shortIDs,
			},
		},
		Sniffing: sniffing{Enabled: true, DestOverride: []string{"http", "tls", "quic"}},
	}

	// Локальный API-инбаунд для чтения статистики (только 127.0.0.1).
	api := apiInbound{
		Tag:      "api",
		Listen:   "127.0.0.1",
		Port:     apiPort,
		Protocol: "dokodemo-door",
		Settings: apiInboundSettings{Address: "127.0.0.1"},
	}

	cfg := xrayConfig{
		Log:    logCfg{Loglevel: "warning"},
		API:    &apiCfg{Tag: "api", Services: []string{"HandlerService", "StatsService"}},
		Stats:  &statsObj{},
		Policy: defaultPolicy(),
		Inbounds: []any{vless, api},
		Outbounds: []outbound{
			{Protocol: "freedom", Tag: "direct"},
			{Protocol: "blackhole", Tag: "block"},
		},
		Routing: &routingCfg{Rules: []routeRule{
			{Type: "field", InboundTag: []string{"api"}, OutboundTag: "api"},
		}},
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func defaultPolicy() *policyCfg {
	return &policyCfg{
		Levels: map[string]levelCfg{
			"0": {StatsUserUplink: true, StatsUserDownlink: true},
		},
		System: systemCfg{
			StatsInboundUplink:   true,
			StatsInboundDownlink: true,
		},
	}
}

// ---- структуры конфига Xray ----

type xrayConfig struct {
	Log       logCfg      `json:"log"`
	API       *apiCfg     `json:"api,omitempty"`
	Stats     *statsObj   `json:"stats,omitempty"`
	Policy    *policyCfg  `json:"policy,omitempty"`
	Inbounds  []any       `json:"inbounds"`
	Outbounds []outbound  `json:"outbounds"`
	Routing   *routingCfg `json:"routing,omitempty"`
}

type logCfg struct {
	Loglevel string `json:"loglevel"`
}

type apiCfg struct {
	Tag      string   `json:"tag"`
	Services []string `json:"services"`
}

type statsObj struct{}

type policyCfg struct {
	Levels map[string]levelCfg `json:"levels"`
	System systemCfg           `json:"system"`
}

type levelCfg struct {
	StatsUserUplink   bool `json:"statsUserUplink"`
	StatsUserDownlink bool `json:"statsUserDownlink"`
}

type systemCfg struct {
	StatsInboundUplink   bool `json:"statsInboundUplink"`
	StatsInboundDownlink bool `json:"statsInboundDownlink"`
}

type routingCfg struct {
	Rules []routeRule `json:"rules"`
}

type routeRule struct {
	Type        string   `json:"type"`
	InboundTag  []string `json:"inboundTag"`
	OutboundTag string   `json:"outboundTag"`
}

type inbound struct {
	Tag            string          `json:"tag"`
	Listen         string          `json:"listen"`
	Port           int             `json:"port"`
	Protocol       string          `json:"protocol"`
	Settings       inboundSettings `json:"settings"`
	StreamSettings streamSettings  `json:"streamSettings"`
	Sniffing       sniffing        `json:"sniffing"`
}

type apiInbound struct {
	Tag      string             `json:"tag"`
	Listen   string             `json:"listen"`
	Port     int                `json:"port"`
	Protocol string             `json:"protocol"`
	Settings apiInboundSettings `json:"settings"`
}

type apiInboundSettings struct {
	Address string `json:"address"`
}

type inboundSettings struct {
	Clients    []client `json:"clients"`
	Decryption string   `json:"decryption"`
}

type client struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Flow  string `json:"flow"`
}

type streamSettings struct {
	Network  string          `json:"network"`
	Security string          `json:"security"`
	Reality  realitySettings `json:"realitySettings"`
}

type realitySettings struct {
	Show        bool     `json:"show"`
	Dest        string   `json:"dest"`
	Xver        int      `json:"xver"`
	ServerNames []string `json:"serverNames"`
	PrivateKey  string   `json:"privateKey"`
	ShortIDs    []string `json:"shortIds"`
}

type sniffing struct {
	Enabled      bool     `json:"enabled"`
	DestOverride []string `json:"destOverride"`
}

type outbound struct {
	Protocol string `json:"protocol"`
	Tag      string `json:"tag"`
}
