package tunnel

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"fmt"
	"time"

	"p2pnode/internal/crypto"
	"p2pnode/internal/protocol"
)

type HandlerConfig struct {
	LocalID      [32]byte
	LocalAddr    string
	LocalPubKey  ed25519.PublicKey
	LocalPrivKey ed25519.PrivateKey

	Dial func(addr string) (RawConn, error)

	Log Logger

	RelayStore   *RelayStore
	DestSessions *DestSessionStore

	Manager *Manager
}

func (hc *HandlerConfig) HandleTunnelBuild(conn RawConn, frame protocol.Frame) error {
	var req protocol.TunnelBuildPayload
	if err := protocol.Decode(frame.Payload, &req); err != nil {
		return fmt.Errorf("tunnel: decode build: %w", err)
	}

	tunnelID := IDFromArray(req.TunnelID)

	if tunnelID.IsZero() {
		return hc.sendBuildFail(conn, frame, tunnelID, "zero tunnel id", hc.LocalID)
	}
	if len(req.FullPath) < 3 {
		return hc.sendBuildFail(conn, frame, tunnelID, "path too short", hc.LocalID)
	}
	if int(req.HopIndex) >= len(req.FullPath) {
		return hc.sendBuildFail(conn, frame, tunnelID, "hop index out of range", hc.LocalID)
	}
	for _, id := range req.PathSoFar {
		if id == hc.LocalID {
			return hc.sendBuildFail(conn, frame, tunnelID, "loop detected", hc.LocalID)
		}
	}

	if int(req.HopIndex) == len(req.FullPath)-1 {
		return hc.handleAsDest(conn, frame, &req, tunnelID)
	}
	return hc.handleAsRelay(conn, frame, &req, tunnelID)
}

func (hc *HandlerConfig) handleAsRelay(conn RawConn, frame protocol.Frame, req *protocol.TunnelBuildPayload, tunnelID ID) error {
	nextIdx := int(req.HopIndex) + 1
	if nextIdx >= len(req.FullPath) {
		return hc.sendBuildFail(conn, frame, tunnelID, "path index out of range", hc.LocalID)
	}
	nextAddr := req.FullPath[nextIdx]

	hc.Log.Info("tunnel: relay build",
		"tunnel_id", tunnelID.Short(),
		"hop", req.HopIndex,
		"next", nextAddr,
		"ttl_sec", req.TTLSec)

	nextConn, err := hc.Dial(nextAddr)
	if err != nil {
		return hc.sendBuildFail(conn, frame, tunnelID,
			fmt.Sprintf("dial next hop %s: %v", nextAddr, err), hc.LocalID)
	}

	ackPayload, _ := protocol.Encode(&protocol.TunnelBuildAckPayload{
		TunnelID: req.TunnelID,
		HopIndex: req.HopIndex,
		HopID:    hc.LocalID,
	})
	if err := conn.WriteFrame(protocol.Frame{
		Version: protocol.Version,
		Type:    protocol.MsgTunnelBuildAck,
		Payload: ackPayload,
	}); err != nil {
		nextConn.Close()
		return fmt.Errorf("tunnel: send build ack: %w", err)
	}

	hc.Log.Info("tunnel: relay build ack sent",
		"tunnel_id", tunnelID.Short(),
		"hop", req.HopIndex,
		"hop_id", fmt.Sprintf("%x", hc.LocalID[:4]),
	)

	nextReq := *req
	nextReq.HopIndex = uint8(nextIdx)
	nextReq.PathSoFar = append(append([][32]byte{}, req.PathSoFar...), hc.LocalID)

	payload, err := protocol.Encode(&nextReq)
	if err != nil {
		nextConn.Close()
		return hc.sendBuildFail(conn, frame, tunnelID, "encode", hc.LocalID)
	}

	if err := nextConn.WriteFrame(protocol.Frame{
		Version: protocol.Version,
		Type:    protocol.MsgTunnelBuild,
		Payload: payload,
	}); err != nil {
		nextConn.Close()
		return hc.sendBuildFail(conn, frame, tunnelID, "write next", hc.LocalID)
	}

	expiresAt := time.Now().Add(time.Duration(req.TTLSec) * time.Second)

	rs := &RelayState{
		TunnelID:  tunnelID,
		PrevConn:  conn,
		NextAddr:  nextAddr,
		NextConn:  nextConn,
		HopIndex:  req.HopIndex,
		ExpiresAt: expiresAt,
	}
	hc.RelayStore.Put(rs)

	go hc.relayLoop(rs)
	return nil
}

func (hc *HandlerConfig) handleAsDest(conn RawConn, frame protocol.Frame, req *protocol.TunnelBuildPayload, tunnelID ID) error {
	hc.Log.Info("tunnel: we are dest",
		"tunnel_id", tunnelID.Short(),
		"init_id", fmt.Sprintf("%x", req.InitID[:4]),
		"ttl_sec", req.TTLSec)

	ephPriv, err := ecdh.X25519().GenerateKey(nil)
	if err != nil {
		return hc.sendBuildFail(conn, frame, tunnelID, "e2e keygen", hc.LocalID)
	}
	ephPub := ephPriv.PublicKey().Bytes()

	randomD, err := crypto.RandomBytes(32)
	if err != nil {
		return hc.sendBuildFail(conn, frame, tunnelID, "random", hc.LocalID)
	}

	transcript, err := tunnelTranscript(tunnelID, req.InitID, hc.LocalID,
		req.E2EPubKey, ephPub, req.E2ERandom, randomD)
	if err != nil {
		return hc.sendBuildFail(conn, frame, tunnelID, "transcript", hc.LocalID)
	}
	sig := ed25519.Sign(hc.LocalPrivKey, transcript)

	initEphPub, err := ecdh.X25519().NewPublicKey(req.E2EPubKey)
	if err != nil {
		return hc.sendBuildFail(conn, frame, tunnelID, "init eph pub", hc.LocalID)
	}
	dh, err := ephPriv.ECDH(initEphPub)
	if err != nil {
		return hc.sendBuildFail(conn, frame, tunnelID, "ecdh", hc.LocalID)
	}
	salt := crypto.Sha256(transcript)
	kDI, err := crypto.HKDFDerive(dh, salt, []byte("dest→initiator"), 32)
	if err != nil {
		return hc.sendBuildFail(conn, frame, tunnelID, "hkdf d→i", hc.LocalID)
	}
	kID, err := crypto.HKDFDerive(dh, salt, []byte("initiator→dest"), 32)
	if err != nil {
		return hc.sendBuildFail(conn, frame, tunnelID, "hkdf i→d", hc.LocalID)
	}
	e2e := &E2ESession{SendKey: kDI, RecvKey: kID}

	expiresAt := time.Now().Add(time.Duration(req.TTLSec) * time.Second)
	hc.DestSessions.Put(tunnelID, &DestSession{
		E2E:       e2e,
		ExpiresAt: expiresAt,
	})

	ok := protocol.TunnelBuildOKPayload{
		TunnelID:      req.TunnelID,
		DestID:        hc.LocalID,
		DestE2EPubKey: ephPub,
		DestE2ERandom: randomD,
		DestIDPubKey:  hc.LocalPubKey,
		Signature:     sig,
		Path:          req.PathSoFar,
	}
	payload, err := protocol.Encode(&ok)
	if err != nil {
		return err
	}
	if err := conn.WriteFrame(protocol.Frame{
		Version: protocol.Version,
		Type:    protocol.MsgTunnelBuildOK,
		Payload: payload,
	}); err != nil {
		return err
	}

	hc.Log.Info("tunnel: build complete as dest",
		"tunnel_id", tunnelID.Short(),
		"ttl_sec", req.TTLSec)

	go hc.destDataLoop(conn, tunnelID, e2e)
	return nil
}

func (hc *HandlerConfig) destDataLoop(conn RawConn, tunnelID ID, e2e *E2ESession) {
	defer conn.Close()
	for {
		frame, err := conn.ReadFrame()
		if err != nil {
			hc.Log.Debug("tunnel: dest read end",
				"tunnel_id", tunnelID.Short(),
				"err", err)
			hc.DestSessions.Remove(tunnelID)
			return
		}
		switch frame.Type {
		case protocol.MsgTunnelData:
			if err := hc.handleDestData(conn, frame, e2e); err != nil {
				hc.Log.Warn("tunnel: handle data", "err", err)
			}
		case protocol.MsgTunnelClose:
			hc.DestSessions.Remove(tunnelID)
			return
		default:
			hc.Log.Debug("tunnel: unexpected frame at dest", "type", frame.Type)
		}
	}
}

func (hc *HandlerConfig) handleDestData(conn RawConn, frame protocol.Frame, e2e *E2ESession) error {
	var data protocol.TunnelDataPayload
	if err := protocol.Decode(frame.Payload, &data); err != nil {
		return fmt.Errorf("decode data: %w", err)
	}

	tunnelID := IDFromArray(data.TunnelID)

	if len(data.Ciphertext) < 8+16 {
		return fmt.Errorf("ciphertext too short: %d", len(data.Ciphertext))
	}
	nonce := data.Ciphertext[:8]
	ct := data.Ciphertext[8:]

	if err := e2e.CheckRecvNonce(bytesToUint64(nonce)); err != nil {
		return fmt.Errorf("nonce: %w", err)
	}

	plaintext, err := openE2E(e2e.RecvKey, nonce, ct, data.TunnelID[:])
	if err != nil {
		return fmt.Errorf("decrypt: %w", err)
	}

	hc.Log.Info("tunnel: message received",
		"tunnel_id", tunnelID.Short(),
		"message_id", fmt.Sprintf("%x", data.MessageID[:4]),
		"size", len(plaintext))

	ack := protocol.TunnelAckPayload{
		TunnelID:  data.TunnelID,
		MessageID: data.MessageID,
	}
	ackPayload, _ := protocol.Encode(&ack)
	return conn.WriteFrame(protocol.Frame{
		Version: protocol.Version,
		Type:    protocol.MsgTunnelAck,
		Payload: ackPayload,
	})
}

func (hc *HandlerConfig) sendBuildFail(conn RawConn, frame protocol.Frame,
	id ID, reason string, failedHop [32]byte) error {
	fail := protocol.TunnelBuildFailPayload{
		TunnelID:  [16]byte(id),
		Reason:    reason,
		FailedHop: failedHop,
	}
	payload, _ := protocol.Encode(&fail)
	_ = conn.WriteFrame(protocol.Frame{
		Version: protocol.Version,
		Type:    protocol.MsgTunnelBuildFail,
		Payload: payload,
	})
	return fmt.Errorf("tunnel: build failed: %s", reason)
}

func (hc *HandlerConfig) relayLoop(rs *RelayState) {
	go func() {
		for {
			frame, err := rs.PrevConn.ReadFrame()
			if err != nil {
				return
			}
			next := rs.GetNextConn()
			if next == nil {
				return
			}
			if err := next.WriteFrame(frame); err != nil {
				return
			}
			if frame.Type == protocol.MsgTunnelClose {
				rs.Close()
				return
			}
		}
	}()

	for {
		next := rs.GetNextConn()
		if next == nil {
			return
		}
		frame, err := next.ReadFrame()
		if err != nil {
			rs.Close()
			return
		}
		if err := rs.PrevConn.WriteFrame(frame); err != nil {
			rs.Close()
			return
		}
		if frame.Type == protocol.MsgTunnelClose {
			rs.Close()
			return
		}
	}
}
