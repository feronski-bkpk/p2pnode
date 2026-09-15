package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	NodeIDLen = 32

	privateKeyFile = "identity.key"
	publicKeyFile  = "identity.pub"

	privateKeyPerm = 0o600
	publicKeyPerm  = 0o644
	stateDirPerm   = 0o700
)

type NodeID [NodeIDLen]byte

func (id NodeID) String() string { return hex.EncodeToString(id[:]) }

func (id NodeID) Short() string { return hex.EncodeToString(id[:4]) }

func (id NodeID) IsZero() bool {
	for _, b := range id {
		if b != 0 {
			return false
		}
	}
	return true
}

type Identity struct {
	PrivateKey ed25519.PrivateKey
	PublicKey  ed25519.PublicKey
	NodeID     NodeID
}

func Generate() (*Identity, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("identity: generate key: %w", err)
	}
	return fromKeys(pub, priv), nil
}

func fromKeys(pub ed25519.PublicKey, priv ed25519.PrivateKey) *Identity {
	return &Identity{
		PrivateKey: priv,
		PublicKey:  pub,
		NodeID:     NodeIDFromPublicKey(pub),
	}
}

func NodeIDFromPublicKey(pub ed25519.PublicKey) NodeID {
	return NodeID(sha256.Sum256(pub))
}

func LoadOrGenerate(dir string) (*Identity, error) {
	if err := os.MkdirAll(dir, stateDirPerm); err != nil {
		return nil, fmt.Errorf("identity: mkdir %s: %w", dir, err)
	}

	privPath := filepath.Join(dir, privateKeyFile)
	pubPath := filepath.Join(dir, publicKeyFile)

	privExists := fileExists(privPath)
	pubExists := fileExists(pubPath)

	if !privExists && !pubExists {
		id, err := Generate()
		if err != nil {
			return nil, err
		}
		if err := id.Save(dir); err != nil {
			return nil, err
		}
		return id, nil
	}
	if privExists != pubExists {
		return nil, fmt.Errorf("identity: inconsistent state in %s: "+
			"one of %s/%s missing", dir, privateKeyFile, publicKeyFile)
	}
	return Load(dir)
}

func Load(dir string) (*Identity, error) {
	privPath := filepath.Join(dir, privateKeyFile)
	pubPath := filepath.Join(dir, publicKeyFile)

	privBytes, err := os.ReadFile(privPath)
	if err != nil {
		return nil, fmt.Errorf("identity: read %s: %w", privPath, err)
	}
	pubBytes, err := os.ReadFile(pubPath)
	if err != nil {
		return nil, fmt.Errorf("identity: read %s: %w", pubPath, err)
	}

	if len(privBytes) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("identity: bad private key size: got %d, want %d",
			len(privBytes), ed25519.PrivateKeySize)
	}
	if len(pubBytes) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("identity: bad public key size: got %d, want %d",
			len(pubBytes), ed25519.PublicKeySize)
	}

	priv := ed25519.PrivateKey(privBytes)
	pub := ed25519.PublicKey(pubBytes)

	derived := priv.Public().(ed25519.PublicKey)
	if !equalBytes(derived, pub) {
		return nil, errors.New("identity: public key does not match private key")
	}

	return fromKeys(pub, priv), nil
}

func (i *Identity) Save(dir string) error {
	if err := os.MkdirAll(dir, stateDirPerm); err != nil {
		return fmt.Errorf("identity: mkdir %s: %w", dir, err)
	}
	privPath := filepath.Join(dir, privateKeyFile)
	pubPath := filepath.Join(dir, publicKeyFile)

	if err := os.WriteFile(privPath, i.PrivateKey, privateKeyPerm); err != nil {
		return fmt.Errorf("identity: write %s: %w", privPath, err)
	}
	if err := os.WriteFile(pubPath, i.PublicKey, publicKeyPerm); err != nil {
		return fmt.Errorf("identity: write %s: %w", pubPath, err)
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
