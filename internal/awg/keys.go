// Package awg — сервер AmneziaWG (обфусцированный WireGuard): ключи, конфиги, применение.
package awg

import (
	"crypto/rand"
	"encoding/base64"

	"golang.org/x/crypto/curve25519"
)

// GenerateKeypair создаёт пару X25519-ключей в формате WireGuard (base64, стандартный алфавит).
func GenerateKeypair() (priv, pub string, err error) {
	var sk [32]byte
	if _, err = rand.Read(sk[:]); err != nil {
		return
	}
	sk[0] &= 248
	sk[31] &= 127
	sk[31] |= 64
	var pk [32]byte
	curve25519.ScalarBaseMult(&pk, &sk)
	return base64.StdEncoding.EncodeToString(sk[:]), base64.StdEncoding.EncodeToString(pk[:]), nil
}
