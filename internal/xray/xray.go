// Package xray собирает config.json для Xray-core (VLESS + Reality)
// из настроек beacon и списка пользователей, и перезапускает сервис.
package xray

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"beacon/internal/config"
	"beacon/internal/store"
)

// Manager управляет конфигом и процессом Xray.
type Manager struct {
	cfg        *config.Config
	store      *store.Store
	configPath string // путь к config.json Xray
	service    string // имя systemd-сервиса Xray
}

// New создаёт менеджер. service обычно "xray".
func New(cfg *config.Config, st *store.Store, configPath, service string) *Manager {
	if service == "" {
		service = "xray"
	}
	return &Manager{cfg: cfg, store: st, configPath: configPath, service: service}
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

	// Reality требует хотя бы один shortId; допускаем и пустой для совместимости клиентов.
	shortIDs := c.ShortIDs
	if len(shortIDs) == 0 {
		shortIDs = []string{GenShortID()}
	}

	cfg := xrayConfig{
		Log: logCfg{Loglevel: "warning"},
		Inbounds: []inbound{{
			Tag:      "vless-reality",
			Listen:   "0.0.0.0",
			Port:     c.VPNPort,
			Protocol: "vless",
			Settings: inboundSettings{
				Clients:    clients,
				Decryption: "none",
			},
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
		}},
		Outbounds: []outbound{
			{Protocol: "freedom", Tag: "direct"},
			{Protocol: "blackhole", Tag: "block"},
		},
	}
	return json.MarshalIndent(cfg, "", "  ")
}

// ---- структуры конфига Xray ----

type xrayConfig struct {
	Log       logCfg     `json:"log"`
	Inbounds  []inbound  `json:"inbounds"`
	Outbounds []outbound `json:"outbounds"`
}

type logCfg struct {
	Loglevel string `json:"loglevel"`
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
