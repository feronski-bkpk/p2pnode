package routing

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"p2pnode/internal/identity"
)

func makeContact(t *testing.T, host string, port uint16) Contact {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	return Contact{
		NodeID:            identity.NodeIDFromPublicKey(pub),
		IdentityAlgorithm: "ed25519",
		IdentityPublicKey: pub,
		Host:              host,
		Port:              port,
	}
}

func TestContactValidate_OK(t *testing.T) {
	c := makeContact(t, "127.0.0.1", 9101)
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestContactValidate_IDMismatch(t *testing.T) {
	c := makeContact(t, "127.0.0.1", 9101)
	c.NodeID[0] ^= 0xFF
	if err := c.Validate(); err == nil {
		t.Fatal("want error for ID mismatch")
	}
}

func TestContactValidate_BadPubKey(t *testing.T) {
	c := makeContact(t, "127.0.0.1", 9101)
	c.IdentityPublicKey = []byte("short")
	if err := c.Validate(); err == nil {
		t.Fatal("want error for bad pubkey")
	}
}

func TestContactAddr(t *testing.T) {
	c := Contact{Host: "127.0.0.1", Port: 9101}
	if c.Addr() != "127.0.0.1:9101" {
		t.Fatalf("Addr: %s", c.Addr())
	}
}

func TestContactSameIdentity(t *testing.T) {
	c := makeContact(t, "127.0.0.1", 9101)
	c2 := c
	c2.Host = "10.0.0.1"
	c2.Port = 9999
	if !c.SameIdentity(c2) {
		t.Fatal("same identity expected (NodeID + pubkey)")
	}
	c3 := makeContact(t, "127.0.0.1", 9101)
	if c.SameIdentity(c3) {
		t.Fatal("different identities expected")
	}
}
