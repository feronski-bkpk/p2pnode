package rpc

import (
	"errors"
	"io"
	"log/slog"

	"p2pnode/internal/events"
	"p2pnode/internal/protocol"
	"p2pnode/internal/routing"
	"p2pnode/internal/transport"
)

type Server struct {
	Local   routing.Contact
	Table   *routing.RoutingTable
	Checker routing.LivenessChecker
	Log     *slog.Logger
	Ev      *events.Logger

	ln transport.Listener
}

func NewServer(local routing.Contact, table *routing.RoutingTable,
	checker routing.LivenessChecker, log *slog.Logger, ev *events.Logger) *Server {
	return &Server{
		Local:   local,
		Table:   table,
		Checker: checker,
		Log:     log,
		Ev:      ev,
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
	defer conn.Close()
	defer func() {
		s.Ev.LogPeer("conn_closed", "", conn.RemoteAddr(), nil)
	}()

	for {
		frame, err := conn.ReadFrame()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				s.Log.Debug("rpc: conn read end", "remote", conn.RemoteAddr(), "err", err)
			}
			return
		}

		s.Ev.LogPeer("frame_recv", "", conn.RemoteAddr(), map[string]any{
			"type":       frame.Type.String(),
			"request_id": frame.RequestID.String(),
			"size":       len(frame.Payload),
		})

		s.dispatch(conn, frame)
	}
}

func (s *Server) dispatch(conn transport.Conn, frame protocol.Frame) {
	var err error
	switch frame.Type {
	case protocol.MsgPing:
		err = HandlePing(s.Local, s.Table, s.Checker, conn, frame)
	case protocol.MsgFindNodeRequest:
		err = HandleFindNode(s.Local, s.Table, s.Checker, conn, frame)
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
