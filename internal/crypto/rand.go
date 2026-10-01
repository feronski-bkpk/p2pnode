package crypto

import (
	"crypto/rand"
	"fmt"
)

func RandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("crypto: rand: %w", err)
	}
	return b, nil
}

func MustRandomBytes(n int) []byte {
	b, err := RandomBytes(n)
	if err != nil {
		panic(err)
	}
	return b
}
