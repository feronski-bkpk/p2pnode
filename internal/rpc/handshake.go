package rpc

import (
	"fmt"
	"time"

	"p2pnode/internal/crypto"
	"p2pnode/internal/protocol"
	"p2pnode/internal/routing"
	"p2pnode/internal/transport"
)

const HandshakeTimeout = 10 * time.Second

func (c *Client) Handshake(addr string, expectedPeerID *routing.ID) (*crypto.SecureConn, error) {
	raw, err := c.tr.Dial(addr)
	if err != nil {
		return nil, fmt.Errorf("rpc: handshake dial: %w", err)
	}

	hs, err := crypto.NewHandshakeState(c.local.PublicKey, c.local.PrivateKey, true)
	if err != nil {
		raw.Close()
		return nil, fmt.Errorf("rpc: handshake state: %w", err)
	}

	deadline := time.Now().Add(HandshakeTimeout)

	var initID [32]byte
	copy(initID[:], c.local.NodeID[:])

	helloBytes, err := hs.BuildHello(initID)
	if err != nil {
		raw.Close()
		return nil, fmt.Errorf("rpc: build hello: %w", err)
	}

	helloPayload, err := protocol.Encode(protocol.HandshakeHelloPayload{Body: helloBytes})
	if err != nil {
		raw.Close()
		return nil, err
	}

	if err := raw.WriteFrame(protocol.Frame{
		Version: protocol.Version,
		Type:    protocol.MsgHandshakeHello,
		Payload: helloPayload,
	}); err != nil {
		raw.Close()
		return nil, fmt.Errorf("rpc: send hello: %w", err)
	}

	replyFrame, err := readFrameWithDeadline(raw, deadline)
	if err != nil {
		raw.Close()
		return nil, fmt.Errorf("rpc: read reply: %w", err)
	}
	if replyFrame.Type != protocol.MsgHandshakeReply {
		raw.Close()
		return nil, fmt.Errorf("rpc: expected reply, got %v", replyFrame.Type)
	}

	var replyPayload protocol.HandshakeReplyPayload
	if err := protocol.Decode(replyFrame.Payload, &replyPayload); err != nil {
		raw.Close()
		return nil, fmt.Errorf("rpc: decode reply: %w", err)
	}

	var replyMsg crypto.ReplyMsg
	if err := protocol.Decode(replyPayload.Body, &replyMsg); err != nil {
		raw.Close()
		return nil, fmt.Errorf("rpc: decode reply body: %w", err)
	}

	if expectedPeerID != nil {
		var gotID routing.ID
		copy(gotID[:], replyMsg.ResponderID[:])
		if gotID != *expectedPeerID {
			raw.Close()
			return nil, fmt.Errorf("rpc: handshake responder id mismatch: got %s, want %s",
				gotID.Short(), expectedPeerID.Short())
		}
	}

	responderID, responderIDPub, responderEph, responderRand, err := hs.ProcessReply(
		replyPayload.Body,
		initID,
		replyMsg.ResponderID,
	)
	if err != nil {
		raw.Close()
		return nil, fmt.Errorf("rpc: process reply: %w", err)
	}

	confirmBytes, err := hs.BuildConfirm(initID, responderID, responderIDPub, responderEph, responderRand)
	if err != nil {
		raw.Close()
		return nil, fmt.Errorf("rpc: build confirm: %w", err)
	}

	confirmPayload, err := protocol.Encode(protocol.HandshakeConfirmPayload{Body: confirmBytes})
	if err != nil {
		raw.Close()
		return nil, err
	}

	if err := raw.WriteFrame(protocol.Frame{
		Version: protocol.Version,
		Type:    protocol.MsgHandshakeConfirm,
		Payload: confirmPayload,
	}); err != nil {
		raw.Close()
		return nil, fmt.Errorf("rpc: send confirm: %w", err)
	}

	transcript, err := crypto.TranscriptBytes(
		initID, responderID,
		c.local.PublicKey, responderIDPub,
		hs.EphemeralPub, responderEph,
		hs.Random, responderRand,
		hs.SessionID,
	)
	if err != nil {
		raw.Close()
		return nil, err
	}

	keys, err := crypto.DeriveKeys(
		hs.EphemeralPriv, responderEph,
		c.local.PrivateKey, responderIDPub,
		transcript, hs.SessionID,
		true,
	)
	if err != nil {
		raw.Close()
		return nil, err
	}

	secure, err := crypto.NewSecureConn(
		raw,
		keys.InitiatorToResponder,
		keys.ResponderToInitiator,
		keys.SessionID,
	)
	if err != nil {
		raw.Close()
		return nil, err
	}

	c.ev.LogPeer("handshake_done", fmtShortID(responderID), addr, map[string]any{
		"role":       "initiator",
		"session_id": keys.SessionID.String(),
	})

	return secure, nil
}

func readFrameWithDeadline(conn transport.Conn, deadline time.Time) (protocol.Frame, error) {
	type result struct {
		f   protocol.Frame
		err error
	}
	ch := make(chan result, 1)
	go func() {
		f, err := conn.ReadFrame()
		ch <- result{f, err}
	}()

	timeout := time.Until(deadline)
	if timeout <= 0 {
		return protocol.Frame{}, fmt.Errorf("rpc: handshake deadline exceeded")
	}

	select {
	case r := <-ch:
		return r.f, r.err
	case <-time.After(timeout):
		return protocol.Frame{}, fmt.Errorf("rpc: handshake read timeout")
	}
}
