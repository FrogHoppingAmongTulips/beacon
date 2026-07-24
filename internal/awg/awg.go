package awg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"beacon/internal/config"
	"beacon/internal/store"
)

// Manager собирает server-side конфиг wg0.conf и применяет его через awg-quick.
type Manager struct {
	cfg        *config.Config
	store      *store.Store
	configPath string
}

func New(cfg *config.Config, st *store.Store, configPath string) *Manager {
	return &Manager{cfg: cfg, store: st, configPath: configPath}
}

// BuildConfig собирает [Interface] с параметрами обфускации и [Peer] на каждого включённого пользователя.
func (m *Manager) BuildConfig() string {
	c := m.cfg
	var b strings.Builder
	fmt.Fprintf(&b, "[Interface]\nPrivateKey = %s\nAddress = %s\nListenPort = %d\n",
		c.AWGPrivateKey, ServerAddr(c.AWGSubnet), c.AWGPort)
	fmt.Fprintf(&b, "Jc = %d\nJmin = %d\nJmax = %d\nS1 = %d\nS2 = %d\nH1 = %d\nH2 = %d\nH3 = %d\nH4 = %d\n",
		c.AWGJc, c.AWGJmin, c.AWGJmax, c.AWGS1, c.AWGS2, c.AWGH1, c.AWGH2, c.AWGH3, c.AWGH4)
	for _, u := range m.store.List() {
		if !u.Enabled || u.AWGPublicKey == "" {
			continue
		}
		fmt.Fprintf(&b, "\n[Peer]\nPublicKey = %s\nAllowedIPs = %s/32\n", u.AWGPublicKey, u.AWGIP)
	}
	return b.String()
}

func (m *Manager) WriteConfig() error {
	if err := os.MkdirAll(filepath.Dir(m.configPath), 0o700); err != nil {
		return err
	}
	return os.WriteFile(m.configPath, []byte(m.BuildConfig()), 0o600)
}

// Apply перезаписывает конфиг и поднимает интерфейс заново (down может упасть, если ещё не был поднят — это ок).
func (m *Manager) Apply() error {
	if err := m.WriteConfig(); err != nil {
		return err
	}
	iface := strings.TrimSuffix(filepath.Base(m.configPath), filepath.Ext(m.configPath))
	_ = exec.Command("awg-quick", "down", iface).Run()
	out, err := exec.Command("awg-quick", "up", iface).CombinedOutput()
	if err != nil {
		return fmt.Errorf("awg-quick up: %w: %s", err, out)
	}
	return nil
}

// Down выключает интерфейс (например, при переключении протокола на Reality).
func (m *Manager) Down() error {
	iface := strings.TrimSuffix(filepath.Base(m.configPath), filepath.Ext(m.configPath))
	return exec.Command("awg-quick", "down", iface).Run()
}
