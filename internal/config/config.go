// Package config хранит постоянные настройки и секреты панели beacon.
package config

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Paths — расположение файлов на диске.
type Paths struct {
	BaseDir    string // каталог beacon, по умолчанию /etc/beacon
	ConfigFile string // config.json
	DataFile   string // users.json
	CertFile   string // самоподписанный TLS-сертификат панели
	KeyFile    string // приватный ключ панели
	XrayConfig string // config.json для Xray-core
}

// DefaultPaths возвращает пути с учётом переменных окружения (удобно для dev).
func DefaultPaths() Paths {
	base := envOr("BEACON_DIR", "/etc/beacon")
	return Paths{
		BaseDir:    base,
		ConfigFile: filepath.Join(base, "config.json"),
		DataFile:   filepath.Join(base, "users.json"),
		CertFile:   filepath.Join(base, "panel-cert.pem"),
		KeyFile:    filepath.Join(base, "panel-key.pem"),
		XrayConfig: envOr("BEACON_XRAY_CONFIG", "/usr/local/etc/xray/config.json"),
	}
}

// Config — постоянные настройки, сериализуется в config.json.
type Config struct {
	// Панель
	ListenAddr   string `json:"listen_addr"`   // адрес веб-панели, напр. ":8443"
	PublicHost   string `json:"public_host"`   // IP/домен сервера для ссылок vless
	PasswordSalt string `json:"password_salt"` // hex
	PasswordHash string `json:"password_hash"` // hex, sha256(salt+password)

	// VPN / Reality
	VPNPort     int      `json:"vpn_port"`    // порт inbound, обычно 443
	PrivateKey  string   `json:"private_key"` // reality x25519 private (base64raw)
	PublicKey   string   `json:"public_key"`  // reality x25519 public (base64raw)
	ShortIDs    []string `json:"short_ids"`   // reality shortIds (hex)
	SNI         string   `json:"sni"`         // маскировочный домен
	Dest        string   `json:"dest"`        // reality dest, host:port
	Fingerprint string   `json:"fingerprint"` // utls fingerprint, напр. chrome

	mu   sync.Mutex
	path string
}

// NewDefault — конфиг с разумными значениями по умолчанию (без секретов).
func NewDefault() *Config {
	return &Config{
		ListenAddr:  ":8443",
		VPNPort:     443,
		SNI:         "www.microsoft.com",
		Dest:        "www.microsoft.com:443",
		Fingerprint: "chrome",
	}
}

// Load читает конфиг с диска.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c := &Config{}
	if err := json.Unmarshal(b, c); err != nil {
		return nil, err
	}
	c.path = path
	return c, nil
}

// Save атомарно пишет конфиг на диск с правами 0600.
func (c *Config) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(c.path, b, 0o600)
}

// SetPath задаёт путь сохранения (для только что созданного конфига).
func (c *Config) SetPath(p string) { c.path = p }

// SetPassword задаёт новый пароль панели (соль + sha256).
func (c *Config) SetPassword(pw string) {
	salt := randHex(16)
	c.PasswordSalt = salt
	c.PasswordHash = hashPw(salt, pw)
}

// CheckPassword сверяет пароль в постоянном времени.
func (c *Config) CheckPassword(pw string) bool {
	if c.PasswordHash == "" {
		return false
	}
	want, _ := hex.DecodeString(c.PasswordHash)
	got, _ := hex.DecodeString(hashPw(c.PasswordSalt, pw))
	return subtle.ConstantTimeCompare(want, got) == 1
}

// ---- helpers ----

func hashPw(salt, pw string) string {
	sum := sha256.Sum256([]byte(salt + ":" + pw))
	return hex.EncodeToString(sum[:])
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// GenPassword генерирует читаемый автопароль.
func GenPassword() string { return randHex(9) }

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// atomicWrite пишет во временный файл и переименовывает — без «полузаписи».
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
