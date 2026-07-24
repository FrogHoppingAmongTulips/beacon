package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"time"

	"aqu/internal/config"
)

// ensureCert загружает существующий сертификат панели или генерирует самоподписанный.
// Браузер покажет предупреждение — это ожидаемо для доступа по IP.
func ensureCert(p config.Paths) (tls.Certificate, error) {
	if fileExists(p.CertFile) && fileExists(p.KeyFile) {
		return tls.LoadX509KeyPair(p.CertFile, p.KeyFile)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "aqu panel"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	// сохраняем, чтобы при рестарте сертификат не менялся
	_ = os.WriteFile(p.CertFile, certPEM, 0o644)
	_ = os.WriteFile(p.KeyFile, keyPEM, 0o600)

	return tls.X509KeyPair(certPEM, keyPEM)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
