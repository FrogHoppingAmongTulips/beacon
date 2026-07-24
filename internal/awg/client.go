package awg

import (
	"fmt"

	"beacon/internal/config"
	"beacon/internal/store"
)

// ClientConfig формирует .conf для устройства пользователя (совместим с Amnezia/WireGuard-клиентами).
func ClientConfig(c *config.Config, u *store.User) string {
	return fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s/32
DNS = 1.1.1.1

Jc = %d
Jmin = %d
Jmax = %d
S1 = %d
S2 = %d
H1 = %d
H2 = %d
H3 = %d
H4 = %d

[Peer]
PublicKey = %s
Endpoint = %s:%d
AllowedIPs = 0.0.0.0/0
PersistentKeepalive = 25
`, u.AWGPrivateKey, u.AWGIP,
		c.AWGJc, c.AWGJmin, c.AWGJmax, c.AWGS1, c.AWGS2, c.AWGH1, c.AWGH2, c.AWGH3, c.AWGH4,
		c.AWGPublicKey, c.PublicHost, c.AWGPort)
}
