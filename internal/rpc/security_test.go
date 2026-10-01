package rpc

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"p2pnode/internal/crypto"
	"p2pnode/internal/identity"
	"p2pnode/internal/protocol"
	"p2pnode/internal/routing"
	"p2pnode/internal/transport/tcp"
)

func TestSecurity_WrongNodeID(t *testing.T) {
	nodes := newTestNetwork(t, 1, 4)
	tr := tcp.New()
	client := NewClient(tr, discardLog(), nil, testIdentity(t))

	var wrongID routing.ID
	wrongID[0] = 0xAA

	_, err := client.Handshake(nodes[0].contact.Addr(), &wrongID)
	if err == nil {
		t.Fatal("handshake with wrong expected peer id should fail")
	}
	t.Logf("got expected error: %v", err)
}

func TestSecurity_TamperedIdentityPub(t *testing.T) {
	initPub, initPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	respPub, respPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}

	var initID, respID [32]byte
	copy(initID[:], initPub)
	copy(respID[:], respPub)

	hsI, err := crypto.NewHandshakeState(initPub, initPriv, true)
	if err != nil {
		t.Fatalf("hsI: %v", err)
	}
	hsR, err := crypto.NewHandshakeState(respPub, respPriv, false)
	if err != nil {
		t.Fatalf("hsR: %v", err)
	}

	hello, err := hsI.BuildHello(initID)
	if err != nil {
		t.Fatalf("BuildHello: %v", err)
	}
	iid, ipub, ieph, irand, err := hsR.ProcessHello(hello)
	if err != nil {
		t.Fatalf("ProcessHello: %v", err)
	}

	replyBytes, err := hsR.BuildReply(iid, ipub, ieph, irand, respID)
	if err != nil {
		t.Fatalf("BuildReply: %v", err)
	}

	var reply crypto.ReplyMsg
	if err := protocol.Decode(replyBytes, &reply); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	otherPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	reply.IdentityPubKey = otherPub
	replyBytes, err = protocol.Encode(&reply)
	if err != nil {
		t.Fatalf("encode reply: %v", err)
	}

	_, _, _, _, err = hsI.ProcessReply(replyBytes, initID, respID)
	if err == nil {
		t.Fatal("handshake should fail when identity pubkey is tampered")
	}
	if !errors.Is(err, crypto.ErrHandshakeBadSignature) {
		t.Logf("got error (not ErrHandshakeBadSignature, but still fail): %v", err)
	}
}

func TestSecurity_TamperedEphemeralPub(t *testing.T) {
	initPub, initPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	respPub, respPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}

	var initID, respID [32]byte
	copy(initID[:], initPub)
	copy(respID[:], respPub)

	hsI, err := crypto.NewHandshakeState(initPub, initPriv, true)
	if err != nil {
		t.Fatalf("hsI: %v", err)
	}
	hsR, err := crypto.NewHandshakeState(respPub, respPriv, false)
	if err != nil {
		t.Fatalf("hsR: %v", err)
	}

	hello, _ := hsI.BuildHello(initID)
	iid, ipub, ieph, irand, err := hsR.ProcessHello(hello)
	if err != nil {
		t.Fatalf("ProcessHello: %v", err)
	}

	replyBytes, err := hsR.BuildReply(iid, ipub, ieph, irand, respID)
	if err != nil {
		t.Fatalf("BuildReply: %v", err)
	}

	var reply crypto.ReplyMsg
	if err := protocol.Decode(replyBytes, &reply); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	newEph, err := crypto.RandomBytes(32)
	if err != nil {
		t.Fatalf("random: %v", err)
	}
	if bytes.Equal(newEph, reply.EphemeralPub) {
		t.Fatal("random eph equals original")
	}
	reply.EphemeralPub = newEph
	replyBytes, err = protocol.Encode(&reply)
	if err != nil {
		t.Fatalf("encode reply: %v", err)
	}

	_, _, _, _, err = hsI.ProcessReply(replyBytes, initID, respID)
	if err == nil {
		t.Fatal("handshake should fail when ephemeral pubkey is tampered")
	}
}

func TestSecurity_FirstFrameNotHello(t *testing.T) {
	nodes := newTestNetwork(t, 1, 4)
	tr := tcp.New()

	conn, err := tr.Dial(nodes[0].contact.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	err = conn.WriteFrame(protocol.Frame{
		Version: protocol.Version,
		Type:    protocol.MsgPing,
		Payload: []byte("fake"),
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	type readResult struct {
		f   protocol.Frame
		err error
	}
	ch := make(chan readResult, 1)
	go func() {
		f, err := conn.ReadFrame()
		ch <- readResult{f, err}
	}()

	select {
	case r := <-ch:
		if r.err == nil {
			if r.f.Type != protocol.MsgError {
				t.Fatalf("expected ERROR, got %v", r.f.Type)
			}
			t.Logf("server returned ERROR as expected")
		} else {
			t.Logf("server closed connection: %v", r.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not respond in time")
	}
}

func TestSecurity_SessionIsolation(t *testing.T) {
	_, _, k1, _, err := cryptoFullHandshake(t)
	if err != nil {
		t.Fatalf("handshake 1: %v", err)
	}
	_, _, k2, _, err := cryptoFullHandshake(t)
	if err != nil {
		t.Fatalf("handshake 2: %v", err)
	}
	if k1.SessionID == k2.SessionID {
		t.Fatal("session ids should differ")
	}
	if bytes.Equal(k1.InitiatorToResponder, k2.InitiatorToResponder) {
		t.Fatal("keys should differ between sessions")
	}
}

func cryptoFullHandshake(t *testing.T) (
	initID, respID [32]byte,
	initKeys *crypto.SessionKeys,
	transcript []byte,
	err error,
) {
	t.Helper()

	initPub, initPriv, kerr := ed25519.GenerateKey(rand.Reader)
	if kerr != nil {
		err = kerr
		return
	}
	respPub, respPriv, kerr := ed25519.GenerateKey(rand.Reader)
	if kerr != nil {
		err = kerr
		return
	}

	copy(initID[:], initPub)
	copy(respID[:], respPub)

	hsI, kerr := crypto.NewHandshakeState(initPub, initPriv, true)
	if kerr != nil {
		err = kerr
		return
	}
	hsR, kerr := crypto.NewHandshakeState(respPub, respPriv, false)
	if kerr != nil {
		err = kerr
		return
	}

	helloBytes, kerr := hsI.BuildHello(initID)
	if kerr != nil {
		err = kerr
		return
	}
	iid, ipub, ieph, irand, kerr := hsR.ProcessHello(helloBytes)
	if kerr != nil {
		err = kerr
		return
	}
	replyBytes, kerr := hsR.BuildReply(iid, ipub, ieph, irand, respID)
	if kerr != nil {
		err = kerr
		return
	}
	_, respIDPub, respEph, respRand, kerr := hsI.ProcessReply(replyBytes, iid, respID)
	if kerr != nil {
		err = kerr
		return
	}
	transcript, kerr = crypto.TranscriptBytes(
		iid, respID,
		initPub, respIDPub,
		hsI.EphemeralPub, respEph,
		hsI.Random, respRand,
		hsI.SessionID,
	)
	if kerr != nil {
		err = kerr
		return
	}
	confirmBytes, kerr := hsI.BuildConfirm(iid, respID, respIDPub, respEph, respRand)
	if kerr != nil {
		err = kerr
		return
	}
	if kerr = hsR.ProcessConfirm(confirmBytes, iid, ipub, respID, ieph, irand); kerr != nil {
		err = kerr
		return
	}
	initKeys, kerr = crypto.DeriveKeys(
		hsI.EphemeralPriv, respEph,
		initPriv, respIDPub,
		transcript, hsI.SessionID,
		true,
	)
	if kerr != nil {
		err = kerr
		return
	}
	return
}

var _ = identity.NodeID{}
