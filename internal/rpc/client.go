package rpc

import (
	"context"
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
	ctx context.Context,
	addr string,
	reqType, respType protocol.MsgType,
	payload []byte,
	timeout time.Duration,
) (protocol.Frame, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		return protocol.Frame{}, errors.New("rpc: call: context has no deadline")
	}

	secure, err := c.Handshake(ctx, addr, nil)
	if err != nil {
		return protocol.Frame{}, fmt.Errorf("rpc: handshake: %w", err)
	}
	defer secure.Close()

	if err := secure.SetDeadline(deadline); err != nil {
		return protocol.Frame{}, fmt.Errorf("rpc: set deadline: %w", err)
	}

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

	f, err := secure.ReadFrame()
	if err != nil {
		return protocol.Frame{}, fmt.Errorf("rpc: read: %w", err)
	}

	c.ev.LogPeer("frame_recv", "", addr, map[string]any{
		"type":       f.Type.String(),
		"request_id": f.RequestID.String(),
		"size":       len(f.Payload),
		"encrypted":  true,
	})
	if f.Type != respType {
		return protocol.Frame{}, fmt.Errorf("rpc: unexpected response type %v, want %v", f.Type, respType)
	}
	if f.RequestID != reqID {
		return protocol.Frame{}, fmt.Errorf("%w: got %s, want %s", ErrRequestIDMismatch, f.RequestID, reqID)
	}
	return f, nil
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
