package protocol

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

func makeContact(t *testing.T) Contact {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	var id [32]byte
	copy(id[:], pub)
	return Contact{
		NodeID:            id,
		IdentityAlgorithm: "ed25519",
		IdentityPublicKey: pub,
		Host:              "127.0.0.1",
		Port:              9101,
	}
}

func TestPingRoundTrip(t *testing.T) {
	c := makeContact(t)
	p := PingPayload{Sender: c, TimestampMs: 12345}
	b, err := Encode(p)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var got PingPayload
	if err := Decode(b, &got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.TimestampMs != p.TimestampMs {
		t.Fatal("timestamp mismatch")
	}
	if got.Sender.Host != c.Host || got.Sender.Port != c.Port {
		t.Fatal("host/port mismatch")
	}
	if !bytes.Equal(got.Sender.IdentityPublicKey, c.IdentityPublicKey) {
		t.Fatal("pubkey mismatch")
	}
}

func TestPongRoundTrip(t *testing.T) {
	c := makeContact(t)
	p := PongPayload{Responder: c, PingTimestampMs: 1, ResponderTimestampMs: 2}
	b, _ := Encode(p)
	var got PongPayload
	if err := Decode(b, &got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.PingTimestampMs != 1 || got.ResponderTimestampMs != 2 {
		t.Fatal("timestamps mismatch")
	}
}

func TestFindNodeRequestRoundTrip(t *testing.T) {
	c := makeContact(t)
	var target [32]byte
	copy(target[:], []byte("target"))
	p := FindNodeRequestPayload{Sender: c, TargetNodeID: target}
	b, _ := Encode(p)
	var got FindNodeRequestPayload
	if err := Decode(b, &got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.TargetNodeID != target {
		t.Fatal("target mismatch")
	}
	if got.Sender.Port != c.Port {
		t.Fatal("port mismatch")
	}
}

func TestFindNodeResponseRoundTrip(t *testing.T) {
	c1 := makeContact(t)
	c2 := makeContact(t)
	var target [32]byte
	p := FindNodeResponsePayload{
		Responder:    c1,
		TargetNodeID: target,
		Contacts:     []Contact{c1, c2},
	}
	b, _ := Encode(p)
	var got FindNodeResponsePayload
	if err := Decode(b, &got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(got.Contacts) != 2 {
		t.Fatalf("contacts: %d", len(got.Contacts))
	}
	if got.Contacts[1].Port != c2.Port {
		t.Fatal("contact port mismatch")
	}
}

func TestErrorRoundTrip(t *testing.T) {
	p := ErrorPayload{Code: "BAD_REQUEST", Message: "malformed"}
	b, _ := Encode(p)
	var got ErrorPayload
	if err := Decode(b, &got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Code != p.Code || got.Message != p.Message {
		t.Fatal("mismatch")
	}
}

func TestDecodeGarbage(t *testing.T) {
	var got PingPayload
	err := Decode([]byte{0xFF, 0xFF, 0xFF}, &got)
	if err == nil {
		t.Fatal("want error")
	}
}

func TestValidateContact_OK(t *testing.T) {
	c := makeContact(t)
	if err := ValidateContact(c); err != nil {
		t.Fatalf("ValidateContact: %v", err)
	}
}

func TestValidateContact_BadPubKey(t *testing.T) {
	c := makeContact(t)
	c.IdentityPublicKey = []byte("short")
	if err := ValidateContact(c); err == nil {
		t.Fatal("want error")
	}
}

func TestValidateContact_BadAlgorithm(t *testing.T) {
	c := makeContact(t)
	c.IdentityAlgorithm = "rsa"
	if err := ValidateContact(c); err == nil {
		t.Fatal("want error")
	}
}

func TestValidateContact_EmptyHost(t *testing.T) {
	c := makeContact(t)
	c.Host = ""
	if err := ValidateContact(c); err == nil {
		t.Fatal("want error")
	}
}

func TestValidateContact_ZeroPort(t *testing.T) {
	c := makeContact(t)
	c.Port = 0
	if err := ValidateContact(c); err == nil {
		t.Fatal("want error")
	}
}
