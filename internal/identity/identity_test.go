package identity

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateDeterministicNodeID(t *testing.T) {
	id, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(id.PublicKey) != ed25519.PublicKeySize {
		t.Fatalf("pubkey size: %d", len(id.PublicKey))
	}
	expected := NodeIDFromPublicKey(id.PublicKey)
	if id.NodeID != expected {
		t.Fatalf("NodeID mismatch: got %s, want %s", id.NodeID, expected)
	}
}

func TestTwoIdentitiesHaveDifferentIDs(t *testing.T) {
	a, _ := Generate()
	b, _ := Generate()
	if a.NodeID == b.NodeID {
		t.Fatal("two identities must have different NodeIDs")
	}
}

func TestLoadOrGenerate_Persistent(t *testing.T) {
	dir := t.TempDir()

	id1, err := LoadOrGenerate(dir)
	if err != nil {
		t.Fatalf("first LoadOrGenerate: %v", err)
	}
	id2, err := LoadOrGenerate(dir)
	if err != nil {
		t.Fatalf("second LoadOrGenerate: %v", err)
	}

	if id1.NodeID != id2.NodeID {
		t.Fatalf("NodeID changed across restart: %s vs %s", id1.NodeID, id2.NodeID)
	}
	if !equalBytes(id1.PublicKey, id2.PublicKey) {
		t.Fatal("public key changed across restart")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	id1, _ := Generate()
	if err := id1.Save(dir); err != nil {
		t.Fatalf("Save: %v", err)
	}
	id2, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if id1.NodeID != id2.NodeID {
		t.Fatalf("NodeID mismatch after round trip")
	}
	if !equalBytes(id1.PrivateKey, id2.PrivateKey) {
		t.Fatal("private key mismatch after round trip")
	}
	if !equalBytes(id1.PublicKey, id2.PublicKey) {
		t.Fatal("public key mismatch after round trip")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := Load(dir)
	if err == nil {
		t.Fatal("Load on empty dir should fail")
	}
}

func TestLoad_CorruptedPublicKey(t *testing.T) {
	dir := t.TempDir()
	id, _ := Generate()
	if err := id.Save(dir); err != nil {
		t.Fatalf("Save: %v", err)
	}
	pubPath := filepath.Join(dir, publicKeyFile)
	if err := os.WriteFile(pubPath, []byte("short"), publicKeyPerm); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := Load(dir)
	if err == nil {
		t.Fatal("Load with corrupted pubkey should fail")
	}
}

func TestLoad_MismatchedPublicAndPrivate(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	idA, _ := Generate()
	idB, _ := Generate()
	_ = idA.Save(dirA)
	_ = idB.Save(dirB)

	privA, _ := os.ReadFile(filepath.Join(dirA, privateKeyFile))
	pubB, _ := os.ReadFile(filepath.Join(dirB, publicKeyFile))

	dirMix := t.TempDir()
	_ = os.WriteFile(filepath.Join(dirMix, privateKeyFile), privA, privateKeyPerm)
	_ = os.WriteFile(filepath.Join(dirMix, publicKeyFile), pubB, publicKeyPerm)

	_, err := Load(dirMix)
	if err == nil {
		t.Fatal("Load with mismatched keys should fail")
	}
}

func TestLoadOrGenerate_InconsistentState(t *testing.T) {
	dir := t.TempDir()
	id, _ := Generate()
	if err := os.WriteFile(filepath.Join(dir, privateKeyFile), id.PrivateKey, privateKeyPerm); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := LoadOrGenerate(dir)
	if err == nil {
		t.Fatal("LoadOrGenerate with inconsistent state should fail")
	}
}

func TestNodeIDShort(t *testing.T) {
	id, _ := Generate()
	s := id.NodeID.Short()
	if len(s) != 8 {
		t.Fatalf("Short len: %d", len(s))
	}
}
