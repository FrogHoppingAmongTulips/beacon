package awg

import (
	"crypto/rand"
	"encoding/binary"
	"math/big"
)

// Params — параметры обфускации AmneziaWG. Случайны на каждый сервер, чтобы усложнить DPI-фингерпринт.
type Params struct {
	Jc, Jmin, Jmax int
	S1, S2         int
	H1, H2, H3, H4 uint32
}

// GenerateParams генерирует случайный набор параметров обфускации.
func GenerateParams() Params {
	seen := map[uint32]bool{}
	next := func() uint32 {
		for {
			v := randMagic()
			if !seen[v] {
				seen[v] = true
				return v
			}
		}
	}
	return Params{
		Jc: randInt(3, 10), Jmin: 40, Jmax: 70,
		S1: randInt(15, 150), S2: randInt(15, 150),
		H1: next(), H2: next(), H3: next(), H4: next(),
	}
}

func randInt(min, max int) int {
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(max-min+1)))
	return min + int(n.Int64())
}

func randMagic() uint32 {
	var b [4]byte
	_, _ = rand.Read(b[:])
	v := binary.BigEndian.Uint32(b[:])
	if v < 5 {
		v += 5
	}
	return v
}
