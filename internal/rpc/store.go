package rpc

import (
	"fmt"
	"time"

	"p2pnode/internal/events"
	"p2pnode/internal/protocol"
	"p2pnode/internal/record"
	"p2pnode/internal/routing"
	"p2pnode/internal/store"
	"p2pnode/internal/transport"
)

const DefaultTTL = 180 * time.Second

func HandleStore(
	local routing.Contact,
	table *routing.RoutingTable,
	st *store.Store,
	checker routing.LivenessChecker,
	conn transport.Conn,
	frame protocol.Frame,
	ev *events.Logger,
) error {
	var req protocol.StoreRequestPayload
	if err := protocol.Decode(frame.Payload, &req); err != nil {
		return writeStoreResp(conn, frame.RequestID, false, "bad payload")
	}

	sender := routing.FromProtocol(req.Sender)
	if err := sender.Validate(); err == nil {
		sender.Touch()
		table.Add(sender, checker)
	}

	rec, err := record.Decode(req.Value)
	if err != nil {
		ev.Log("store_rejected", map[string]any{
			"key":    fmt.Sprintf("%x", req.Key[:8]),
			"reason": "decode: " + err.Error(),
		})
		return writeStoreResp(conn, frame.RequestID, false, "bad record")
	}

	ttl := time.Duration(req.TTLSec) * time.Second
	if ttl <= 0 || ttl > DefaultTTL {
		ttl = DefaultTTL
	}

	now := time.Now()
	key := store.ID(req.Key)
	inserted, err := st.Put(key, rec, now, ttl)
	if err != nil {
		ev.Log("store_rejected", map[string]any{
			"key":    key.Short(),
			"reason": err.Error(),
		})
		return writeStoreResp(conn, frame.RequestID, false, err.Error())
	}

	ev.Log("store_accepted", map[string]any{
		"key":      key.Short(),
		"inserted": inserted,
		"seq":      rec.SequenceNumber,
		"size":     st.Size(),
	})

	return writeStoreResp(conn, frame.RequestID, true, "")
}

func writeStoreResp(conn transport.Conn, reqID protocol.RequestID, ok bool, msg string) error {
	payload, err := protocol.Encode(protocol.StoreResponsePayload{OK: ok, Message: msg})
	if err != nil {
		return err
	}
	return conn.WriteFrame(protocol.Frame{
		Version:   protocol.Version,
		Type:      protocol.MsgStoreResponse,
		RequestID: reqID,
		Payload:   payload,
	})
}

func HandleFindValue(
	local routing.Contact,
	table *routing.RoutingTable,
	st *store.Store,
	checker routing.LivenessChecker,
	conn transport.Conn,
	frame protocol.Frame,
	ev *events.Logger,
) error {
	var req protocol.FindValueRequestPayload
	if err := protocol.Decode(frame.Payload, &req); err != nil {
		return writeFindValueRespNodes(conn, frame.RequestID, table, req.Key)
	}

	sender := routing.FromProtocol(req.Sender)
	if err := sender.Validate(); err == nil {
		sender.Touch()
		table.Add(sender, checker)
	}

	key := store.ID(req.Key)
	now := time.Now()
	rec, found := st.Get(key, now)
	if found {
		value, err := rec.Encode()
		if err != nil {
			ev.Log("findvalue_encode_error", map[string]any{
				"key": key.Short(),
				"err": err.Error(),
			})
			return writeFindValueRespNodes(conn, frame.RequestID, table, req.Key)
		}
		ev.Log("findvalue_found", map[string]any{
			"key": key.Short(),
		})
		return writeFindValueRespValue(conn, frame.RequestID, value)
	}

	ev.Log("findvalue_notfound", map[string]any{
		"key": key.Short(),
	})
	return writeFindValueRespNodes(conn, frame.RequestID, table, req.Key)
}

func writeFindValueRespValue(conn transport.Conn, reqID protocol.RequestID, value []byte) error {
	payload, err := protocol.Encode(protocol.FindValueResponsePayload{
		Found: true,
		Value: value,
	})
	if err != nil {
		return err
	}
	return conn.WriteFrame(protocol.Frame{
		Version:   protocol.Version,
		Type:      protocol.MsgFindValueResponse,
		RequestID: reqID,
		Payload:   payload,
	})
}

func writeFindValueRespNodes(conn transport.Conn, reqID protocol.RequestID, table *routing.RoutingTable, key [32]byte) error {
	target := routing.ID(key)
	closest := table.Closest(target, table.K())

	contacts := make([]protocol.Contact, 0, len(closest))
	for _, c := range closest {
		contacts = append(contacts, c.ToProtocol())
	}

	payload, err := protocol.Encode(protocol.FindValueResponsePayload{
		Found: false,
		Nodes: contacts,
	})
	if err != nil {
		return err
	}
	return conn.WriteFrame(protocol.Frame{
		Version:   protocol.Version,
		Type:      protocol.MsgFindValueResponse,
		RequestID: reqID,
		Payload:   payload,
	})
}

func (c *Client) StoreRPC(
	local routing.Contact,
	target routing.Contact,
	key store.ID,
	value []byte,
	ttl time.Duration,
	timeout time.Duration,
) (bool, error) {
	req := protocol.StoreRequestPayload{
		Sender: local.ToProtocol(),
		Key:    [32]byte(key),
		Value:  value,
		TTLSec: int64(ttl.Seconds()),
	}
	payload, err := protocol.Encode(req)
	if err != nil {
		return false, err
	}

	frame, err := c.Call(target.Addr(), protocol.MsgStoreRequest, protocol.MsgStoreResponse, payload, timeout)
	if err != nil {
		return false, err
	}

	var resp protocol.StoreResponsePayload
	if err := protocol.Decode(frame.Payload, &resp); err != nil {
		return false, err
	}
	return resp.OK, nil
}

func (c *Client) FindValueRPC(
	local routing.Contact,
	target routing.Contact,
	key store.ID,
	timeout time.Duration,
) ([]byte, bool, []routing.Contact, error) {
	req := protocol.FindValueRequestPayload{
		Sender: local.ToProtocol(),
		Key:    [32]byte(key),
	}
	payload, err := protocol.Encode(req)
	if err != nil {
		return nil, false, nil, err
	}

	frame, err := c.Call(target.Addr(), protocol.MsgFindValueRequest, protocol.MsgFindValueResponse, payload, timeout)
	if err != nil {
		return nil, false, nil, err
	}

	var resp protocol.FindValueResponsePayload
	if err := protocol.Decode(frame.Payload, &resp); err != nil {
		return nil, false, nil, err
	}

	if resp.Found {
		return resp.Value, true, nil, nil
	}

	out := make([]routing.Contact, 0, len(resp.Nodes))
	seen := make(map[routing.ID]bool)
	for _, p := range resp.Nodes {
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
	return nil, false, out, nil
}
