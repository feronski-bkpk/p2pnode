package crypto

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"github.com/vmihailenco/msgpack/v5"
)

func fullHandshake(t *testing.T) (
	initID, respID [32]byte,
	initKeys, respKeys *SessionKeys,
	transcript []byte,
	err error,
) {
	t.Helper()

	initPub, initPriv, _ := ed25519.GenerateKey(rand.Reader)
	respPub, respPriv, _ := ed25519.GenerateKey(rand.Reader)

	copy(initID[:], initPub)
	copy(respID[:], respPub)

	hsI, err := NewHandshakeState(initPub, initPriv, true)
	if err != nil {
		return
	}
	hsR, err := NewHandshakeState(respPub, respPriv, false)
	if err != nil {
		return
	}

	helloBytes, err := hsI.BuildHello(initID)
	if err != nil {
		return
	}
	initIDOut, initIDPub, initEph, initRand, err := hsR.ProcessHello(helloBytes)
	if err != nil {
		return
	}
	initID = initIDOut

	replyBytes, err := hsR.BuildReply(initID, initIDPub, initEph, initRand, respID)
	if err != nil {
		return
	}
	respIDOut, respIDPub, respEph, respRand, err := hsI.ProcessReply(replyBytes, initID, respID)
	if err != nil {
		return
	}
	respID = respIDOut

	transcript, err = TranscriptBytes(
		initID, respID,
		initPub, respPub,
		hsI.EphemeralPub, respEph,
		hsI.Random, respRand,
		hsI.SessionID,
	)
	if err != nil {
		return
	}

	confirmBytes, err := hsI.BuildConfirm(initID, respID, respIDPub, respEph, respRand)
	if err != nil {
		return
	}
	if err = hsR.ProcessConfirm(confirmBytes, initID, initIDPub, respID, initEph, initRand); err != nil {
		return
	}

	initKeys, err = DeriveKeys(hsI.EphemeralPriv, respEph, initPriv, respPub, transcript, hsI.SessionID, true)
	if err != nil {
		return
	}
	respKeys, err = DeriveKeys(hsR.EphemeralPriv, initEph, respPriv, initPub, transcript, hsR.SessionID, false)
	if err != nil {
		return
	}
	return
}

func TestHandshake_Success(t *testing.T) {
	_, _, initKeys, respKeys, _, err := fullHandshake(t)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}

	if !bytes.Equal(initKeys.InitiatorToResponder, respKeys.InitiatorToResponder) {
		t.Fatal("InitiatorToResponder mismatch")
	}
	if !bytes.Equal(initKeys.ResponderToInitiator, respKeys.ResponderToInitiator) {
		t.Fatal("ResponderToInitiator mismatch")
	}
	if initKeys.SessionID != respKeys.SessionID {
		t.Fatal("session_id mismatch")
	}
}

func TestHandshake_BadReplySignature(t *testing.T) {
	initPub, initPriv, _ := ed25519.GenerateKey(rand.Reader)
	respPub, respPriv, _ := ed25519.GenerateKey(rand.Reader)

	var initID, respID [32]byte
	copy(initID[:], initPub)
	copy(respID[:], respPub)

	hsI, _ := NewHandshakeState(initPub, initPriv, true)
	hsR, _ := NewHandshakeState(respPub, respPriv, false)

	hello, _ := hsI.BuildHello(initID)
	iid, ipub, ieph, irand, err := hsR.ProcessHello(hello)
	if err != nil {
		t.Fatalf("ProcessHello: %v", err)
	}

	replyBytes, _ := hsR.BuildReply(iid, ipub, ieph, irand, respID)

	var reply ReplyMsg
	if err := msgpack.Unmarshal(replyBytes, &reply); err != nil {
		t.Fatalf("unmarshal reply: %v", err)
	}
	reply.Signature[0] ^= 0xFF
	replyBytes, _ = msgpack.Marshal(&reply)

	_, _, _, _, err = hsI.ProcessReply(replyBytes, initID, respID)
	if err == nil {
		t.Fatal("want error for bad signature")
	}
}

func TestHandshake_WrongResponderID(t *testing.T) {
	initPub, initPriv, _ := ed25519.GenerateKey(rand.Reader)
	respPub, respPriv, _ := ed25519.GenerateKey(rand.Reader)

	var initID, respID, wrongID [32]byte
	copy(initID[:], initPub)
	copy(respID[:], respPub)
	wrongID[0] = 0xAA

	hsI, _ := NewHandshakeState(initPub, initPriv, true)
	hsR, _ := NewHandshakeState(respPub, respPriv, false)

	hello, _ := hsI.BuildHello(initID)
	iid, ipub, ieph, irand, _ := hsR.ProcessHello(hello)
	reply, _ := hsR.BuildReply(iid, ipub, ieph, irand, respID)

	_, _, _, _, err := hsI.ProcessReply(reply, initID, wrongID)
	if err == nil {
		t.Fatal("want error for wrong responder id")
	}
}

func TestHandshake_WrongConfirmSignature(t *testing.T) {
	initPub, initPriv, _ := ed25519.GenerateKey(rand.Reader)
	respPub, respPriv, _ := ed25519.GenerateKey(rand.Reader)

	var initID, respID [32]byte
	copy(initID[:], initPub)
	copy(respID[:], respPub)

	hsI, _ := NewHandshakeState(initPub, initPriv, true)
	hsR, _ := NewHandshakeState(respPub, respPriv, false)

	hello, _ := hsI.BuildHello(initID)
	iid, ipub, ieph, irand, _ := hsR.ProcessHello(hello)
	reply, _ := hsR.BuildReply(iid, ipub, ieph, irand, respID)
	_, respIDPub, respEph, respRand, _ := hsI.ProcessReply(reply, initID, respID)

	confirmBytes, _ := hsI.BuildConfirm(initID, respID, respIDPub, respEph, respRand)

	var confirm ConfirmMsg
	if err := msgpack.Unmarshal(confirmBytes, &confirm); err != nil {
		t.Fatalf("unmarshal confirm: %v", err)
	}
	confirm.Signature[0] ^= 0xFF
	confirmBytes, _ = msgpack.Marshal(&confirm)

	err := hsR.ProcessConfirm(confirmBytes, initID, ipub, respID, ieph, irand)
	if err == nil {
		t.Fatal("want error for bad confirm signature")
	}
}

func TestHandshake_DifferentSessions(t *testing.T) {
	_, _, k1, _, _, err := fullHandshake(t)
	if err != nil {
		t.Fatalf("handshake 1: %v", err)
	}
	_, _, k2, _, _, err := fullHandshake(t)
	if err != nil {
		t.Fatalf("handshake 2: %v", err)
	}
	if k1.SessionID == k2.SessionID {
		t.Fatal("session_id should differ between handshakes")
	}
	if bytes.Equal(k1.InitiatorToResponder, k2.InitiatorToResponder) {
		t.Fatal("keys should differ between handshakes")
	}
}

func TestDeriveKeys_Symmetric(t *testing.T) {
	_, _, initKeys, respKeys, _, err := fullHandshake(t)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if !bytes.Equal(initKeys.InitiatorToResponder, respKeys.InitiatorToResponder) {
		t.Fatal("k_i→r mismatch between initiator and responder")
	}
	if !bytes.Equal(initKeys.ResponderToInitiator, respKeys.ResponderToInitiator) {
		t.Fatal("k_r→i mismatch")
	}
}
