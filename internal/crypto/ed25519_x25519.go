package crypto

import (
	"crypto/ed25519"
	"crypto/sha512"
	"fmt"

	"filippo.io/edwards25519"
)

func ed25519PublicKeyToX25519Bytes(pub ed25519.PublicKey) ([]byte, error) {
	if len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("crypto: bad ed25519 pubkey size %d", len(pub))
	}
	p, err := new(edwards25519.Point).SetBytes(pub)
	if err != nil {
		return nil, fmt.Errorf("crypto: edwards25519 parse: %w", err)
	}
	return p.BytesMontgomery(), nil
}

func ed25519PrivateKeyToX25519Bytes(priv ed25519.PrivateKey) ([]byte, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("crypto: bad ed25519 privkey size %d", len(priv))
	}
	seed := priv.Seed()
	h := sha512.Sum512(seed)
	x := make([]byte, 32)
	copy(x, h[:32])
	x[0] &= 248
	x[31] &= 127
	x[31] |= 64
	return x, nil
}
