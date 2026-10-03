package rpc

import (
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"p2pnode/internal/events"
	"p2pnode/internal/identity"
	"p2pnode/internal/protocol"
	"p2pnode/internal/transport"
)

type Client struct {
	tr    transport.Transport
	log   *slog.Logger
	ev    *events.Logger
	local *identity.Identity
}

func NewClient(tr transport.Transport, log *slog.Logger, ev *events.Logger, local *identity.Identity) *Client {
	return &Client{tr: tr, log: log, ev: ev, local: local}
}

func (c *Client) Call(
	addr string,
	reqType, respType protocol.MsgType,
	payload []byte,
	timeout time.Duration,
) (protocol.Frame, error) {
	secure, err := c.Handshake(addr, nil)
	if err != nil {
		return protocol.Frame{}, fmt.Errorf("rpc: handshake: %w", err)
	}
	defer secure.Close()

	reqID, err := newRequestID()
	if err != nil {
		return protocol.Frame{}, fmt.Errorf("rpc: request id: %w", err)
	}

	c.ev.LogPeer("frame_sent", "", addr, map[string]any{
		"type":       reqType.String(),
		"request_id": reqID.String(),
		"size":       len(payload),
		"encrypted":  true,
	})

	if err := secure.WriteFrame(protocol.Frame{
		Version:   protocol.Version,
		Type:      reqType,
		RequestID: reqID,
		Payload:   payload,
	}); err != nil {
		return protocol.Frame{}, fmt.Errorf("rpc: write: %w", err)
	}

	type result struct {
		f   protocol.Frame
		err error
	}
	ch := make(chan result, 1)
	go func() {
		f, err := secure.ReadFrame()
		ch <- result{f, err}
	}()

	select {
	case r := <-ch:
		if r.err != nil {
			return protocol.Frame{}, fmt.Errorf("rpc: read: %w", r.err)
		}
		c.ev.LogPeer("frame_recv", "", addr, map[string]any{
			"type":       r.f.Type.String(),
			"request_id": r.f.RequestID.String(),
			"size":       len(r.f.Payload),
			"encrypted":  true,
		})
		if r.f.Type != respType {
			return protocol.Frame{}, fmt.Errorf("rpc: unexpected response type %v, want %v",
				r.f.Type, respType)
		}
		if r.f.RequestID != reqID {
			return protocol.Frame{}, fmt.Errorf("%w: got %s, want %s",
				ErrRequestIDMismatch, r.f.RequestID, reqID)
		}
		return r.f, nil

	case <-time.After(timeout):
		_ = secure.Close()
		return protocol.Frame{}, transport.ErrTimeout
	}
}

var ErrRequestIDMismatch = errors.New("rpc: request_id mismatch")

func newRequestID() (protocol.RequestID, error) {
	var id protocol.RequestID
	if _, err := rand.Read(id[:]); err != nil {
		return id, err
	}
	return id, nil
}

func (c *Client) DialRaw(addr string) (transport.Conn, error) {
	return c.tr.Dial(addr)
}
