package rpc

import (
	"errors"
	"io"
	"log/slog"
	"time"

	"p2pnode/internal/events"
	"p2pnode/internal/identity"
	"p2pnode/internal/protocol"
	"p2pnode/internal/routing"
	"p2pnode/internal/store"
	"p2pnode/internal/transport"
)

type Server struct {
	Local    routing.Contact
	Identity *identity.Identity
	Table    *routing.RoutingTable
	Store    *store.Store
	Checker  routing.LivenessChecker
	Log      *slog.Logger
	Ev       *events.Logger

	TunnelHandler func(conn transport.Conn, frame protocol.Frame) error

	ln transport.Listener
}

func NewServer(
	local routing.Contact,
	id *identity.Identity,
	table *routing.RoutingTable,
	st *store.Store,
	checker routing.LivenessChecker,
	log *slog.Logger,
	ev *events.Logger,
) *Server {
	return &Server{
		Local:    local,
		Identity: id,
		Table:    table,
		Store:    st,
		Checker:  checker,
		Log:      log,
		Ev:       ev,
	}
}

func (s *Server) Serve(ln transport.Listener) {
	s.ln = ln
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, transport.ErrClosed) {
				return
			}
			s.Log.Debug("rpc: accept end", "err", err)
			return
		}
		s.Ev.LogPeer("conn_accepted", "", conn.RemoteAddr(), nil)
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn transport.Conn) {
	defer func() {
		s.Ev.LogPeer("conn_closed", "", conn.RemoteAddr(), nil)
	}()

	firstFrame, err := conn.ReadFrame()
	if err != nil {
		if !errors.Is(err, io.EOF) {
			s.Log.Debug("rpc: first read failed", "remote", conn.RemoteAddr(), "err", err)
		}
		_ = conn.Close()
		return
	}

	switch firstFrame.Type {
	case protocol.MsgTunnelBuild, protocol.MsgTunnelData,
		protocol.MsgTunnelBuildOK, protocol.MsgTunnelBuildFail,
		protocol.MsgTunnelBuildAck, protocol.MsgTunnelAck,
		protocol.MsgTunnelClose:

		if sc, ok := conn.(interface{ SetReadTimeout(time.Duration) }); ok {
			sc.SetReadTimeout(0)
			s.Log.Debug("rpc: tunnel conn read timeout disabled",
				"remote", conn.RemoteAddr(),
				"type", firstFrame.Type.String())
		}

		if s.TunnelHandler == nil {
			s.Log.Warn("rpc: tunnel handler not set",
				"remote", conn.RemoteAddr(),
				"type", firstFrame.Type.String())
			_ = writeError(conn, firstFrame.RequestID, "TUNNEL_UNSUPPORTED",
				"tunnel handler not set")
			_ = conn.Close()
			return
		}

		if err := s.TunnelHandler(conn, firstFrame); err != nil {
			s.Log.Debug("rpc: tunnel handler error",
				"remote", conn.RemoteAddr(),
				"type", firstFrame.Type.String(),
				"err", err)
			_ = conn.Close()
		}
		return
	}

	defer conn.Close()

	if firstFrame.Type != protocol.MsgHandshakeHello {
		s.Log.Warn("rpc: expected handshake hello",
			"remote", conn.RemoteAddr(),
			"got", firstFrame.Type.String())
		_ = writeError(conn, firstFrame.RequestID, "EXPECTED_HANDSHAKE",
			"first frame must be HANDSHAKE_HELLO")
		return
	}

	s.Ev.LogPeer("handshake_start", "", conn.RemoteAddr(), nil)

	secure, peerID, err := s.handleHandshake(conn, firstFrame)
	if err != nil {
		s.Log.Warn("rpc: handshake failed", "remote", conn.RemoteAddr(), "err", err)
		s.Ev.LogPeer("handshake_failed", "", conn.RemoteAddr(), map[string]any{"err": err.Error()})
		return
	}
	defer secure.Close()

	peerIDShort := fmtShortID(peerID)

	for {
		frame, err := secure.ReadFrame()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				s.Log.Debug("rpc: secure read end", "remote", secure.RemoteAddr(), "err", err)
			}
			return
		}
		s.Ev.LogPeer("frame_recv", peerIDShort, secure.RemoteAddr(), map[string]any{
			"type":       frame.Type.String(),
			"request_id": frame.RequestID.String(),
			"size":       len(frame.Payload),
		})
		s.dispatch(secure, frame)
	}
}

func (s *Server) dispatch(conn transport.Conn, frame protocol.Frame) {
	var err error
	switch frame.Type {
	case protocol.MsgPing:
		err = HandlePing(s.Local, s.Table, s.Checker, conn, frame)
	case protocol.MsgFindNodeRequest:
		err = HandleFindNode(s.Local, s.Table, s.Checker, conn, frame)
	case protocol.MsgStoreRequest:
		err = HandleStore(s.Local, s.Table, s.Store, s.Checker, conn, frame, s.Ev)
	case protocol.MsgFindValueRequest:
		err = HandleFindValue(s.Local, s.Table, s.Store, s.Checker, conn, frame, s.Ev)
	default:
		_ = writeError(conn, frame.RequestID, "UNKNOWN_TYPE", frame.Type.String())
		s.Ev.LogPeer("handler_error", "", conn.RemoteAddr(), map[string]any{
			"type": frame.Type.String(),
			"err":  "unknown type",
		})
		return
	}
	if err != nil {
		s.Log.Warn("rpc: handler error",
			"type", frame.Type,
			"request_id", frame.RequestID.String(),
			"err", err)
		s.Ev.LogPeer("handler_error", "", conn.RemoteAddr(), map[string]any{
			"type": frame.Type.String(),
			"err":  err.Error(),
		})
	}
}

func writeError(conn transport.Conn, reqID protocol.RequestID, code, msg string) error {
	payload, err := protocol.Encode(protocol.ErrorPayload{Code: code, Message: msg})
	if err != nil {
		return err
	}
	return conn.WriteFrame(protocol.Frame{
		Version:   protocol.Version,
		Type:      protocol.MsgError,
		RequestID: reqID,
		Payload:   payload,
	})
}

func fmtShortID(id [32]byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, 8)
	for i := 0; i < 4; i++ {
		out[i*2] = hexdigits[id[i]>>4]
		out[i*2+1] = hexdigits[id[i]&0x0F]
	}
	return string(out)
}
