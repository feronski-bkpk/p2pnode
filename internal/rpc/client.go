package rpc

import (
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"p2pnode/internal/events"
	"p2pnode/internal/protocol"
	"p2pnode/internal/transport"
)

type Client struct {
	tr  transport.Transport
	log *slog.Logger
	ev  *events.Logger
}

func NewClient(tr transport.Transport, log *slog.Logger, ev *events.Logger) *Client {
	return &Client{tr: tr, log: log, ev: ev}
}

type callResult struct {
	frame protocol.Frame
	err   error
}

func (c *Client) Call(addr string, reqType, respType protocol.MsgType, payload []byte, timeout time.Duration) (protocol.Frame, error) {
	reqID, err := newRequestID()
	if err != nil {
		return protocol.Frame{}, fmt.Errorf("rpc: request id: %w", err)
	}

	conn, err := c.tr.Dial(addr)
	if err != nil {
		c.ev.LogPeer("conn_error", "", addr, map[string]any{
			"phase": "dial",
			"err":   err.Error(),
		})
		return protocol.Frame{}, fmt.Errorf("rpc: dial %s: %w", addr, err)
	}
	defer conn.Close()

	c.ev.LogPeer("conn_dialed", "", addr, map[string]any{
		"remote": conn.RemoteAddr(),
	})

	reqFrame := protocol.Frame{
		Version:   protocol.Version,
		Type:      reqType,
		RequestID: reqID,
		Payload:   payload,
	}
	if err := conn.WriteFrame(reqFrame); err != nil {
		c.ev.LogPeer("conn_error", "", addr, map[string]any{
			"phase": "write",
			"err":   err.Error(),
		})
		return protocol.Frame{}, fmt.Errorf("rpc: write: %w", err)
	}

	c.ev.LogPeer("frame_sent", "", addr, map[string]any{
		"type":       reqType.String(),
		"request_id": reqID.String(),
		"size":       len(payload),
	})

	ch := make(chan callResult, 1)
	go func() {
		f, err := conn.ReadFrame()
		ch <- callResult{f, err}
	}()

	select {
	case r := <-ch:
		if r.err != nil {
			c.ev.LogPeer("conn_error", "", addr, map[string]any{
				"phase": "read",
				"err":   r.err.Error(),
			})
			return protocol.Frame{}, fmt.Errorf("rpc: read: %w", r.err)
		}
		c.ev.LogPeer("frame_recv", "", addr, map[string]any{
			"type":       r.frame.Type.String(),
			"request_id": r.frame.RequestID.String(),
			"size":       len(r.frame.Payload),
		})
		if r.frame.Type != respType {
			return protocol.Frame{}, fmt.Errorf("rpc: unexpected response type %v, want %v",
				r.frame.Type, respType)
		}
		if r.frame.RequestID != reqID {
			return protocol.Frame{}, fmt.Errorf("%w: got %s, want %s",
				ErrRequestIDMismatch, r.frame.RequestID, reqID)
		}
		return r.frame, nil

	case <-time.After(timeout):
		_ = conn.Close()
		c.ev.LogPeer("conn_error", "", addr, map[string]any{
			"phase": "timeout",
		})
		return protocol.Frame{}, transport.ErrTimeout
	}
}

var (
	ErrRequestIDMismatch = errors.New("rpc: request_id mismatch")
)

func newRequestID() (protocol.RequestID, error) {
	var id protocol.RequestID
	if _, err := rand.Read(id[:]); err != nil {
		return id, err
	}
	return id, nil
}
