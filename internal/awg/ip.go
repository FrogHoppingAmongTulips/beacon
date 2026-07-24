package awg

import (
	"fmt"
	"net"
	"strings"
)

// NextIP находит первый свободный адрес в подсети (.2 и выше — .1 зарезервирован за сервером).
func NextIP(subnet string, used map[string]bool) (string, error) {
	_, ipnet, err := net.ParseCIDR(subnet)
	if err != nil {
		return "", err
	}
	ip := ipnet.IP.To4()
	if ip == nil {
		return "", fmt.Errorf("нужна ipv4 подсеть")
	}
	for i := 2; i < 254; i++ {
		cand := fmt.Sprintf("%d.%d.%d.%d", ip[0], ip[1], ip[2], i)
		if !used[cand] {
			return cand, nil
		}
	}
	return "", fmt.Errorf("подсеть исчерпана")
}

// ServerAddr — адрес сервера в подсети (первый хост, .1) с маской из subnet.
func ServerAddr(subnet string) string {
	parts := strings.SplitN(subnet, "/", 2)
	ipParts := strings.Split(parts[0], ".")
	if len(ipParts) == 4 {
		ipParts[3] = "1"
	}
	host := strings.Join(ipParts, ".")
	if len(parts) == 2 {
		return host + "/" + parts[1]
	}
	return host
}
