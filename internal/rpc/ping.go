package rpc

import (
	"time"

	"p2pnode/internal/protocol"
	"p2pnode/internal/routing"
	"p2pnode/internal/transport"
)

func (c *Client) Ping(local routing.Contact, addr string, expectedID *routing.ID, timeout time.Duration) (routing.Contact, error) {
	payload, err := protocol.Encode(protocol.PingPayload{
		Sender:      local.ToProtocol(),
		TimestampMs: uint64(time.Now().UnixMilli()),
	})
	if err != nil {
		return routing.Contact{}, err
	}

	frame, err := c.Call(addr, protocol.MsgPing, protocol.MsgPong, payload, timeout)
	if err != nil {
		return routing.Contact{}, err
	}

	var pong protocol.PongPayload
	if err := protocol.Decode(frame.Payload, &pong); err != nil {
		return routing.Contact{}, err
	}

	responder := routing.FromProtocol(pong.Responder)
	if err := responder.Validate(); err != nil {
		return routing.Contact{}, err
	}
	if expectedID != nil && responder.NodeID != *expectedID {
		return routing.Contact{}, ErrWrongResponder
	}
	responder.MarkVerified()
	return responder, nil
}

var ErrWrongResponder = errWrongResponder{}

type errWrongResponder struct{}

func (errWrongResponder) Error() string { return "rpc: wrong responder node_id" }

func HandlePing(
	local routing.Contact,
	table *routing.RoutingTable,
	checker routing.LivenessChecker,
	conn transport.Conn,
	frame protocol.Frame,
) error {
	var ping protocol.PingPayload
	if err := protocol.Decode(frame.Payload, &ping); err != nil {
		return err
	}
	sender := routing.FromProtocol(ping.Sender)
	if err := sender.Validate(); err == nil {
		sender.Touch()
		table.Add(sender, checker)
	}

	resp := protocol.PongPayload{
		Responder:            local.ToProtocol(),
		PingTimestampMs:      ping.TimestampMs,
		ResponderTimestampMs: uint64(time.Now().UnixMilli()),
	}
	payload, err := protocol.Encode(resp)
	if err != nil {
		return err
	}
	return conn.WriteFrame(protocol.Frame{
		Version:   protocol.Version,
		Type:      protocol.MsgPong,
		RequestID: frame.RequestID,
		Payload:   payload,
	})
}
