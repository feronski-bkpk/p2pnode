package crypto

import (
	"crypto/cipher"
	"errors"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

const (
	AEADKeyLen = chacha20poly1305.KeySize // 32

	AEADNonceLen = chacha20poly1305.NonceSize // 12

	AEADTagLen = chacha20poly1305.Overhead // 16
)

type AEAD struct {
	aead cipher.AEAD
}

func NewAEAD(key []byte) (*AEAD, error) {
	if len(key) != AEADKeyLen {
		return nil, fmt.Errorf("crypto: bad AEAD key size %d", len(key))
	}
	a, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: new chacha20poly1305: %w", err)
	}
	return &AEAD{aead: a}, nil
}

func (a *AEAD) Seal(nonce, plaintext, aad []byte) ([]byte, error) {
	if len(nonce) != AEADNonceLen {
		return nil, fmt.Errorf("crypto: bad nonce size %d", len(nonce))
	}
	return a.aead.Seal(nil, nonce, plaintext, aad), nil
}

func (a *AEAD) Open(nonce, ciphertext, aad []byte) ([]byte, error) {
	if len(nonce) != AEADNonceLen {
		return nil, fmt.Errorf("crypto: bad nonce size %d", len(nonce))
	}
	plaintext, err := a.aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, ErrAEADAuth
	}
	return plaintext, nil
}

var ErrAEADAuth = errors.New("crypto: aead authentication failed")
