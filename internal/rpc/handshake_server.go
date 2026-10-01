package rpc

import (
	"fmt"

	"p2pnode/internal/crypto"
	"p2pnode/internal/protocol"
	"p2pnode/internal/transport"
)

func (s *Server) handleHandshake(raw transport.Conn, helloFrame protocol.Frame) (*crypto.SecureConn, [32]byte, error) {
	var helloPayload protocol.HandshakeHelloPayload
	if err := protocol.Decode(helloFrame.Payload, &helloPayload); err != nil {
		return nil, [32]byte{}, fmt.Errorf("rpc: decode hello: %w", err)
	}

	hs, err := crypto.NewHandshakeState(s.Identity.PublicKey, s.Identity.PrivateKey, false)
	if err != nil {
		return nil, [32]byte{}, fmt.Errorf("rpc: handshake state: %w", err)
	}

	initiatorID, initiatorIDPub, initiatorEph, initiatorRand, err := hs.ProcessHello(helloPayload.Body)
	if err != nil {
		return nil, [32]byte{}, fmt.Errorf("rpc: process hello: %w", err)
	}

	var localNodeID [32]byte
	copy(localNodeID[:], s.Identity.NodeID[:])

	replyBytes, err := hs.BuildReply(initiatorID, initiatorIDPub, initiatorEph, initiatorRand, localNodeID)
	if err != nil {
		return nil, [32]byte{}, fmt.Errorf("rpc: build reply: %w", err)
	}

	replyPayload, err := protocol.Encode(protocol.HandshakeReplyPayload{Body: replyBytes})
	if err != nil {
		return nil, [32]byte{}, err
	}

	if err := raw.WriteFrame(protocol.Frame{
		Version:   protocol.Version,
		Type:      protocol.MsgHandshakeReply,
		RequestID: helloFrame.RequestID,
		Payload:   replyPayload,
	}); err != nil {
		return nil, [32]byte{}, fmt.Errorf("rpc: send reply: %w", err)
	}

	confirmFrame, err := raw.ReadFrame()
	if err != nil {
		return nil, [32]byte{}, fmt.Errorf("rpc: read confirm: %w", err)
	}
	if confirmFrame.Type != protocol.MsgHandshakeConfirm {
		return nil, [32]byte{}, fmt.Errorf("rpc: expected confirm, got %v", confirmFrame.Type)
	}

	var confirmPayload protocol.HandshakeConfirmPayload
	if err := protocol.Decode(confirmFrame.Payload, &confirmPayload); err != nil {
		return nil, [32]byte{}, fmt.Errorf("rpc: decode confirm: %w", err)
	}

	if err := hs.ProcessConfirm(
		confirmPayload.Body,
		initiatorID,
		initiatorIDPub,
		localNodeID,
		initiatorEph,
		initiatorRand,
	); err != nil {
		return nil, [32]byte{}, fmt.Errorf("rpc: process confirm: %w", err)
	}

	transcript, err := crypto.TranscriptBytes(
		initiatorID, localNodeID,
		initiatorIDPub, s.Identity.PublicKey,
		initiatorEph, hs.EphemeralPub,
		initiatorRand, hs.Random,
		hs.SessionID,
	)
	if err != nil {
		return nil, [32]byte{}, err
	}

	keys, err := crypto.DeriveKeys(
		hs.EphemeralPriv, initiatorEph,
		s.Identity.PrivateKey, initiatorIDPub,
		transcript, hs.SessionID,
		false,
	)
	if err != nil {
		return nil, [32]byte{}, err
	}

	secure, err := crypto.NewSecureConn(
		raw,
		keys.ResponderToInitiator,
		keys.InitiatorToResponder,
		keys.SessionID,
	)
	if err != nil {
		return nil, [32]byte{}, err
	}

	s.Ev.LogPeer("handshake_done", fmtShortID(initiatorID), raw.RemoteAddr(), map[string]any{
		"role":       "responder",
		"session_id": keys.SessionID.String(),
	})

	return secure, initiatorID, nil
}
