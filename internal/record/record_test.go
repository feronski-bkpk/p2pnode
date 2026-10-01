package record

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"
)

func makeKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	return pub, priv
}

func TestNew_SignAndValidate(t *testing.T) {
	_, priv := makeKey(t)
	r, err := New(priv, []string{"127.0.0.1:9001"}, 3*time.Minute, 1)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := r.Validate(time.Now()); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestCanonicalBytes_Deterministic(t *testing.T) {
	_, priv := makeKey(t)
	r, _ := New(priv, []string{"127.0.0.1:9001"}, 3*time.Minute, 1)

	b1, err := r.CanonicalBytes()
	if err != nil {
		t.Fatalf("CanonicalBytes: %v", err)
	}
	b2, err := r.CanonicalBytes()
	if err != nil {
		t.Fatalf("CanonicalBytes: %v", err)
	}
	if string(b1) != string(b2) {
		t.Fatal("canonical encoding is not deterministic")
	}
}

func TestSign_ChangesSignature(t *testing.T) {
	_, priv := makeKey(t)
	r, _ := New(priv, []string{"127.0.0.1:9001"}, 3*time.Minute, 1)
	sig1 := append([]byte(nil), r.Signature...)

	r.SequenceNumber = 2
	if err := r.Sign(priv); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if string(sig1) == string(r.Signature) {
		t.Fatal("signature should change after field change")
	}
}

func TestValidate_BadNodeID(t *testing.T) {
	_, priv := makeKey(t)
	r, _ := New(priv, []string{"127.0.0.1:9001"}, 3*time.Minute, 1)
	r.NodeID[0] ^= 0xFF
	if err := r.Validate(time.Now()); err == nil {
		t.Fatal("want error")
	}
}

func TestValidate_BadPubKeySize(t *testing.T) {
	_, priv := makeKey(t)
	r, _ := New(priv, []string{"127.0.0.1:9001"}, 3*time.Minute, 1)
	r.IdentityPubKey = r.IdentityPubKey[:16]
	if err := r.Validate(time.Now()); err == nil {
		t.Fatal("want error")
	}
}

func TestValidate_NoAddresses(t *testing.T) {
	_, priv := makeKey(t)
	r, _ := New(priv, []string{"127.0.0.1:9001"}, 3*time.Minute, 1)
	r.Addresses = nil
	if err := r.Validate(time.Now()); err == nil {
		t.Fatal("want error")
	}
}

func TestValidate_TooManyAddresses(t *testing.T) {
	_, priv := makeKey(t)
	addrs := make([]string, MaxAddresses+1)
	for i := range addrs {
		addrs[i] = "127.0.0.1:9001"
	}
	r, _ := New(priv, addrs, 3*time.Minute, 1)
	if err := r.Validate(time.Now()); err == nil {
		t.Fatal("want error")
	}
}

func TestValidate_BadAddress(t *testing.T) {
	cases := []string{
		"",
		"no-port",
		"127.0.0.1:",
		":9001",
		"127.0.0.1:0",
		"127.0.0.1:99999",
		"127.0.0.1:abc",
	}
	for _, a := range cases {
		_, priv := makeKey(t)
		r, _ := New(priv, []string{a}, 3*time.Minute, 1)
		if err := r.Validate(time.Now()); err == nil {
			t.Errorf("address %q should be invalid", a)
		}
	}
}

func TestValidate_Expired(t *testing.T) {
	_, priv := makeKey(t)
	r, _ := New(priv, []string{"127.0.0.1:9001"}, 1*time.Second, 1)
	future := time.Now().Add(2 * time.Second)
	if err := r.Validate(future); err == nil {
		t.Fatal("want error: expired")
	}
}

func TestValidate_NotYetValid(t *testing.T) {
	_, priv := makeKey(t)
	r, _ := New(priv, []string{"127.0.0.1:9001"}, 3*time.Minute, 1)
	past := time.Now().Add(-1 * time.Hour)
	if err := r.Validate(past); err == nil {
		t.Fatal("want error: not yet valid")
	}
}

func TestValidate_BadSignature(t *testing.T) {
	_, priv := makeKey(t)
	r, _ := New(priv, []string{"127.0.0.1:9001"}, 3*time.Minute, 1)
	r.Signature[0] ^= 0xFF
	if err := r.Validate(time.Now()); err == nil {
		t.Fatal("want error: bad signature")
	}
}

func TestValidate_SequenceZero(t *testing.T) {
	_, priv := makeKey(t)
	r, _ := New(priv, []string{"127.0.0.1:9001"}, 3*time.Minute, 0)
	if err := r.Validate(time.Now()); err == nil {
		t.Fatal("want error: sequence_number=0")
	}
}

func TestValidate_AliasTooLong(t *testing.T) {
	_, priv := makeKey(t)
	r, _ := New(priv, []string{"127.0.0.1:9001"}, 3*time.Minute, 1)
	r.Alias = strings.Repeat("x", MaxAliasLen+1)
	if err := r.Validate(time.Now()); err == nil {
		t.Fatal("want error: alias too long")
	}
}

func TestEncode_Decode_RoundTrip(t *testing.T) {
	_, priv := makeKey(t)
	r1, _ := New(priv, []string{"127.0.0.1:9001", "10.0.0.1:9001"}, 3*time.Minute, 42)

	b, err := r1.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	r2, err := Decode(b)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if r2.NodeID != r1.NodeID {
		t.Fatal("node_id mismatch")
	}
	if r2.SequenceNumber != r1.SequenceNumber {
		t.Fatal("sequence_number mismatch")
	}
	if len(r2.Addresses) != len(r1.Addresses) {
		t.Fatal("addresses count mismatch")
	}
	if string(r2.Signature) != string(r1.Signature) {
		t.Fatal("signature mismatch")
	}
	if err := r2.Validate(time.Now()); err != nil {
		t.Fatalf("round-tripped record invalid: %v", err)
	}
}

func TestNew_DeterministicNodeID(t *testing.T) {
	pub, priv := makeKey(t)
	r, _ := New(priv, []string{"127.0.0.1:9001"}, 3*time.Minute, 1)
	expected := NodeIDFromPublicKey(pub)
	if r.NodeID != expected {
		t.Fatal("NodeID should be SHA-256(pubkey)")
	}
}

func TestValidAddress(t *testing.T) {
	valid := []string{
		"127.0.0.1:9001",
		"localhost:80",
		"[::1]:9001",
		"example.com:443",
	}
	for _, a := range valid {
		if !validAddress(a) {
			t.Errorf("%q should be valid", a)
		}
	}
	invalid := []string{
		"",
		"no-port",
		"127.0.0.1:",
		":9001",
		"127.0.0.1:0",
		"127.0.0.1:99999",
		"127.0.0.1:abc",
	}
	for _, a := range invalid {
		if validAddress(a) {
			t.Errorf("%q should be invalid", a)
		}
	}
}
