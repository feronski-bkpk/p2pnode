package tunnel

import (
	"encoding/binary"
	"fmt"

	"p2pnode/internal/crypto"
)

func openE2E(key, nonce, ct, aad []byte) ([]byte, error) {
	fullNonce := make([]byte, 12)
	copy(fullNonce[4:], nonce)

	aead, err := crypto.NewAEAD(key)
	if err != nil {
		return nil, err
	}
	return aead.Open(fullNonce, ct, aad)
}

func sealE2E(key []byte, counter uint64, plaintext, aad []byte) ([]byte, error) {
	nonce := make([]byte, 12)
	binary.BigEndian.PutUint64(nonce[4:], counter)

	aead, err := crypto.NewAEAD(key)
	if err != nil {
		return nil, err
	}
	ct, err := aead.Seal(nonce, plaintext, aad)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, 8+len(ct))
	out = append(out, nonce[4:]...)
	out = append(out, ct...)
	return out, nil
}

func bytesToUint64(b []byte) uint64 {
	if len(b) < 8 {
		return 0
	}
	return binary.BigEndian.Uint64(b[:8])
}

var _ = fmt.Sprintf
