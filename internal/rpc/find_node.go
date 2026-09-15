package rpc

import (
	"fmt"
	"sort"
	"time"

	"p2pnode/internal/protocol"
	"p2pnode/internal/routing"
	"p2pnode/internal/transport"
)

func (c *Client) FindNodeRPC(local routing.Contact, target routing.Contact, targetID routing.ID, timeout time.Duration) ([]routing.Contact, error) {
	req := protocol.FindNodeRequestPayload{
		Sender:       local.ToProtocol(),
		TargetNodeID: targetID,
	}
	payload, err := protocol.Encode(req)
	if err != nil {
		return nil, err
	}

	frame, err := c.Call(target.Addr(), protocol.MsgFindNodeRequest, protocol.MsgFindNodeResponse, payload, timeout)
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
) error {
	var req protocol.FindNodeRequestPayload
	if err := protocol.Decode(frame.Payload, &req); err != nil {
		return err
	}
	sender := routing.FromProtocol(req.Sender)
	if err := sender.Validate(); err == nil {
		sender.Touch()
		table.Add(sender, checker)
	}

	target := routing.ID(req.TargetNodeID)
	closest := table.Closest(target, table.K())

	all := make([]routing.Contact, 0, len(closest)+1)
	all = append(all, local)
	all = append(all, closest...)

	sort.Slice(all, func(i, j int) bool {
		return routing.CloserTo(target, all[i].NodeID, all[j].NodeID)
	})

	seen := make(map[routing.ID]bool, len(all))
	contacts := make([]protocol.Contact, 0, table.K())
	for _, c := range all {
		if seen[c.NodeID] {
			continue
		}
		seen[c.NodeID] = true
		contacts = append(contacts, c.ToProtocol())
		if len(contacts) >= table.K() {
			break
		}
	}

	resp := protocol.FindNodeResponsePayload{
		Responder:    local.ToProtocol(),
		TargetNodeID: req.TargetNodeID,
		Contacts:     contacts,
	}
	payload, err := protocol.Encode(resp)
	if err != nil {
		return err
	}
	return conn.WriteFrame(protocol.Frame{
		Version:   protocol.Version,
		Type:      protocol.MsgFindNodeResponse,
		RequestID: frame.RequestID,
		Payload:   payload,
	})
}
