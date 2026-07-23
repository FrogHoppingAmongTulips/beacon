package xray

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
)

// GenerateX25519 создаёт пару ключей Reality (X25519) в формате base64raw-url,
// как их ждёт Xray (эквивалент вывода `xray x25519`).
func GenerateX25519() (priv, pub string, err error) {
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	priv = base64.RawURLEncoding.EncodeToString(k.Bytes())
	pub = base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes())
	return priv, pub, nil
}

// GenShortID генерирует shortId Reality (8 байт hex).
func GenShortID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
