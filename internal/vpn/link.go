// Package vpn собирает ссылку подключения vless:// для клиентов (v2rayNG, Streisand, Hiddify).
package vpn

import (
	"net"
	"net/url"
	"strconv"

	"beacon/internal/config"
	"beacon/internal/store"
)

// Link формирует строку vless://uuid@host:port?...#name для пользователя.
func Link(cfg *config.Config, u *store.User) string {
	q := url.Values{}
	q.Set("type", "tcp")
	q.Set("security", "reality")
	q.Set("encryption", "none")
	q.Set("pbk", cfg.PublicKey)
	q.Set("fp", cfg.Fingerprint)
	q.Set("sni", cfg.SNI)
	q.Set("flow", "xtls-rprx-vision")
	if len(cfg.ShortIDs) > 0 {
		q.Set("sid", cfg.ShortIDs[0])
	}

	link := url.URL{
		Scheme:   "vless",
		User:     url.User(u.ID),
		Host:     net.JoinHostPort(cfg.PublicHost, strconv.Itoa(cfg.VPNPort)),
		RawQuery: q.Encode(),
		Fragment: u.Name, // имя ключа, отобразится в приложении
	}
	return link.String()
}
