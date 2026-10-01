package crypto

import (
	"crypto/sha256"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

func HKDFDerive(ikm, salt, info []byte, keyLen int) ([]byte, error) {
	if keyLen <= 0 || keyLen > 32*255 {
		return nil, fmt.Errorf("crypto: invalid key length %d", keyLen)
	}
	r := hkdf.New(sha256.New, ikm, salt, info)
	out := make([]byte, keyLen)
	if _, err := io.ReadFull(r, out); err != nil {
		return nil, fmt.Errorf("crypto: hkdf: %w", err)
	}
	return out, nil
}

func Sha256(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}
