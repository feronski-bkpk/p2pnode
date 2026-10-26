package rpc

import (
	"context"
	"fmt"
	"time"

	"p2pnode/internal/protocol"
	"p2pnode/internal/routing"
	"p2pnode/internal/transport"
)

func (c *Client) FindNodeRPC(ctx context.Context, local routing.Contact, target routing.Contact, targetID routing.ID, timeout time.Duration) ([]routing.Contact, error) {
	req := protocol.FindNodeRequestPayload{
		Sender:       local.ToProtocol(),
		TargetNodeID: targetID,
	}
	payload, err := protocol.Encode(req)
	if err != nil {
		return nil, err
	}

	frame, err := c.Call(ctx, target.Addr(), protocol.MsgFindNodeRequest, protocol.MsgFindNodeResponse, payload, timeout)
	if err != nil {
		return nil, err
	}

	var resp protocol.FindNodeResponsePayload
	if err := protocol.Decode(frame.Payload, &resp); err != nil {
		return nil, err
	}

	responder := routing.FromProtocol(resp.Responder)
	if err := responder.Validate(); err != nil {
		return nil, fmt.Errorf("rpc: invalid responder: %w", err)
	}
	if responder.NodeID != target.NodeID {
		return nil, ErrWrongResponder
	}
	if resp.TargetNodeID != [32]byte(targetID) {
		return nil, fmt.Errorf("rpc: target mismatch in response")
	}

	out := make([]routing.Contact, 0, len(resp.Contacts))
	seen := make(map[routing.ID]bool)
	for _, p := range resp.Contacts {
		ct := routing.FromProtocol(p)
		if err := ct.Validate(); err != nil {
			continue
		}
		if ct.NodeID == local.NodeID {
			continue
		}
		if seen[ct.NodeID] {
			continue
		}
		seen[ct.NodeID] = true
		out = append(out, ct)
	}
	return out, nil
}

func HandleFindNode(
	local routing.Contact,
	table *routing.RoutingTable,
	checker routing.LivenessChecker,
	conn transport.Conn,
	frame protocol.Frame,
	sybil *SybilConfig,
) error {
	var req protocol.FindNodeRequestPayload
	if err := protocol.Decode(frame.Payload, &req); err != nil {
		return writeFindNodeResp(conn, frame.RequestID, local, req.TargetNodeID, nil)
	}

	sender := routing.FromProtocol(req.Sender)
	if err := sender.Validate(); err == nil {
		sender.Touch()
		table.Add(sender, checker)
	}

	target := routing.ID(req.TargetNodeID)

	if sybil != nil && sybil.Enabled && sybil.Victim != "" {
		victim, err := routing.IDFromHex(sybil.Victim)
		if err == nil && target == victim {
			return writeFindNodeResp(conn, frame.RequestID, local, req.TargetNodeID,
				[]routing.Contact{local})
		}
	}

	closest := table.Closest(target, table.K())
	contacts := make([]routing.Contact, 0, len(closest))
	for _, c := range closest {
		if c.NodeID == local.NodeID {
			continue
		}
		contacts = append(contacts, c)
	}
	if len(contacts) > table.K() {
		contacts = contacts[:table.K()]
	}

	return writeFindNodeResp(conn, frame.RequestID, local, req.TargetNodeID, contacts)
}

func writeFindNodeResp(
	conn transport.Conn,
	reqID protocol.RequestID,
	local routing.Contact,
	target [32]byte,
	contacts []routing.Contact,
) error {
	protos := make([]protocol.Contact, 0, len(contacts))
	for _, c := range contacts {
		protos = append(protos, c.ToProtocol())
	}

	payload, err := protocol.Encode(protocol.FindNodeResponsePayload{
		Responder:    local.ToProtocol(),
		TargetNodeID: target,
		Contacts:     protos,
	})
	if err != nil {
		return fmt.Errorf("rpc: encode find_node response: %w", err)
	}

	return conn.WriteFrame(protocol.Frame{
		Version:   protocol.Version,
		Type:      protocol.MsgFindNodeResponse,
		RequestID: reqID,
		Payload:   payload,
	})
}
