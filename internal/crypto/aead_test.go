package crypto

import (
	"bytes"
	"testing"
)

func TestHKDF_Deterministic(t *testing.T) {
	ikm := []byte("input key material")
	salt := []byte("salt")
	info := []byte("context")

	k1, err := HKDFDerive(ikm, salt, info, 32)
	if err != nil {
		t.Fatalf("HKDFDerive: %v", err)
	}
	k2, err := HKDFDerive(ikm, salt, info, 32)
	if err != nil {
		t.Fatalf("HKDFDerive: %v", err)
	}
	if !bytes.Equal(k1, k2) {
		t.Fatal("HKDF not deterministic")
	}
}

func TestHKDF_DifferentInfo(t *testing.T) {
	ikm := []byte("ikm")
	salt := []byte("salt")

	k1, _ := HKDFDerive(ikm, salt, []byte("a"), 32)
	k2, _ := HKDFDerive(ikm, salt, []byte("b"), 32)
	if bytes.Equal(k1, k2) {
		t.Fatal("different info must give different keys")
	}
}

func TestHKDF_BadKeyLen(t *testing.T) {
	_, err := HKDFDerive([]byte("x"), []byte("y"), []byte("z"), 0)
	if err == nil {
		t.Fatal("want error for zero key length")
	}
	_, err = HKDFDerive([]byte("x"), []byte("y"), []byte("z"), -1)
	if err == nil {
		t.Fatal("want error for negative key length")
	}
}

func TestSHA256(t *testing.T) {
	h := Sha256([]byte("abc"))
	expected := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if len(h) != 32 {
		t.Fatalf("hash length: %d", len(h))
	}
	const hexdigits = "0123456789abcdef"
	got := ""
	for _, b := range h {
		got += string(hexdigits[b>>4]) + string(hexdigits[b&0x0F])
	}
	if got != expected {
		t.Fatalf("hash: got %s, want %s", got, expected)
	}
}

func TestRandomBytes(t *testing.T) {
	b1, err := RandomBytes(32)
	if err != nil {
		t.Fatalf("RandomBytes: %v", err)
	}
	b2, _ := RandomBytes(32)
	if bytes.Equal(b1, b2) {
		t.Fatal("two random calls returned same bytes")
	}
	if len(b1) != 32 {
		t.Fatalf("length: %d", len(b1))
	}
}

func TestAEAD_SealOpen(t *testing.T) {
	key := MustRandomBytes(32)
	a, err := NewAEAD(key)
	if err != nil {
		t.Fatalf("NewAEAD: %v", err)
	}
	nonce := MustRandomBytes(AEADNonceLen)
	plaintext := []byte("hello")
	aad := []byte("header")

	ct, err := a.Seal(nonce, plaintext, aad)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	got, err := a.Open(nonce, ct, aad)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatal("plaintext mismatch")
	}
}

func TestAEAD_WrongAAD(t *testing.T) {
	key := MustRandomBytes(32)
	a, _ := NewAEAD(key)
	nonce := MustRandomBytes(AEADNonceLen)
	ct, _ := a.Seal(nonce, []byte("data"), []byte("aad1"))

	_, err := a.Open(nonce, ct, []byte("aad2"))
	if err == nil {
		t.Fatal("want error for wrong AAD")
	}
}

func TestAEAD_WrongKey(t *testing.T) {
	k1 := MustRandomBytes(32)
	k2 := MustRandomBytes(32)
	a1, _ := NewAEAD(k1)
	a2, _ := NewAEAD(k2)
	nonce := MustRandomBytes(AEADNonceLen)
	ct, _ := a1.Seal(nonce, []byte("data"), nil)

	_, err := a2.Open(nonce, ct, nil)
	if err == nil {
		t.Fatal("want error for wrong key")
	}
}

func TestAEAD_BadKeySize(t *testing.T) {
	_, err := NewAEAD([]byte("short"))
	if err == nil {
		t.Fatal("want error for bad key size")
	}
}

func TestAEAD_BadNonceSize(t *testing.T) {
	key := MustRandomBytes(32)
	a, _ := NewAEAD(key)
	_, err := a.Seal([]byte("short"), []byte("data"), nil)
	if err == nil {
		t.Fatal("want error for bad nonce size")
	}
}

func TestAEAD_TamperedCiphertext(t *testing.T) {
	key := MustRandomBytes(32)
	a, _ := NewAEAD(key)
	nonce := MustRandomBytes(AEADNonceLen)
	ct, _ := a.Seal(nonce, []byte("data"), nil)

	ct[len(ct)-1] ^= 0xFF
	_, err := a.Open(nonce, ct, nil)
	if err == nil {
		t.Fatal("want error for tampered ciphertext")
	}
}
