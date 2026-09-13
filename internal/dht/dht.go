package dht

import (
	"errors"
	"fmt"
	"log/slog"

	"p2pnode/internal/transport"
)

type DHT struct {
	SelfID   ID
	SelfAddr string

	table *RoutingTable

	tr  transport.Transport
	log *slog.Logger
}

func New(selfID ID, selfAddr string, tr transport.Transport, log *slog.Logger) *DHT {
	return &DHT{
		SelfID:   selfID,
		SelfAddr: selfAddr,
		table:    NewRoutingTable(selfID),
		tr:       tr,
		log:      log,
	}
}

func (d *DHT) Table() *RoutingTable { return d.table }

// === RPC-хелперы ===

func (d *DHT) rpc(addr string, reqType transport.MsgType, reqPayload []byte, respType transport.MsgType) (transport.Frame, error) {
	conn, err := d.tr.Dial(addr)
	if err != nil {
		return transport.Frame{}, fmt.Errorf("dht: dial %s: %w", addr, err)
	}
	defer conn.Close()

	if err := conn.WriteFrame(transport.Frame{Type: reqType, Payload: reqPayload}); err != nil {
		return transport.Frame{}, fmt.Errorf("dht: write: %w", err)
	}

	f, err := conn.ReadFrame()
	if err != nil {
		return transport.Frame{}, fmt.Errorf("dht: read: %w", err)
	}
	if f.Type != respType {
		return transport.Frame{}, fmt.Errorf("dht: unexpected response type %v, want %v", f.Type, respType)
	}
	return f, nil
}

func (d *DHT) Ping(n Node) (PingResponse, error) {
	req, err := Encode(PingRequest{FromID: d.SelfID, FromAddr: d.SelfAddr})
	if err != nil {
		return PingResponse{}, err
	}
	f, err := d.rpc(n.Addr, transport.MsgPing, req, transport.MsgPong)
	if err != nil {
		return PingResponse{}, err
	}
	var resp PingResponse
	if err := Decode(f.Payload, &resp); err != nil {
		return PingResponse{}, err
	}

	n.ID = resp.FromID
	n.Addr = resp.FromAddr
	n.Touch()
	d.table.Add(n)
	return resp, nil
}

func (d *DHT) FindNodeRPC(n Node, target ID) ([]Node, error) {
	req, err := Encode(FindNodeRequest{
		FromID:   d.SelfID,
		FromAddr: d.SelfAddr,
		Target:   target,
	})
	if err != nil {
		return nil, err
	}
	f, err := d.rpc(n.Addr, transport.MsgFindNode, req, transport.MsgFindNode)
	if err != nil {
		return nil, err
	}
	var resp FindNodeResponse
	if err := Decode(f.Payload, &resp); err != nil {
		return nil, err
	}
	n.Touch()
	d.table.Add(n)
	return resp.Nodes, nil
}

// === Итеративный поиск ===

func (d *DHT) FindNode(target ID) []Node {
	shortlist := d.table.Closest(target, K)
	if len(shortlist) == 0 {
		return nil
	}

	queried := make(map[ID]bool)

	for iter := 0; iter < IDBits; iter++ {
		var toQuery []Node
		for _, n := range shortlist {
			if !queried[n.ID] {
				toQuery = append(toQuery, n)
				queried[n.ID] = true
				if len(toQuery) >= Alpha {
					break
				}
			}
		}
		if len(toQuery) == 0 {
			break
		}

		type reply struct {
			from  Node
			nodes []Node
			err   error
		}
		ch := make(chan reply, len(toQuery))
		for _, n := range toQuery {
			go func(n Node) {
				nodes, err := d.FindNodeRPC(n, target)
				ch <- reply{from: n, nodes: nodes, err: err}
			}(n)
		}

		var newNodes []Node
		for range toQuery {
			r := <-ch
			if r.err != nil {
				d.log.Debug("find_node rpc failed", "peer", r.from.Addr, "err", r.err)
				d.table.Remove(r.from.ID)
				continue
			}
			newNodes = append(newNodes, r.nodes...)
		}

		for _, n := range newNodes {
			if n.ID == d.SelfID || n.ID.IsZero() {
				continue
			}
			shortlist = append(shortlist, n)
			d.table.Add(n)
		}

		shortlist = uniqueSorted(shortlist, target, K)

		allQueried := true
		for _, n := range shortlist {
			if !queried[n.ID] {
				allQueried = false
				break
			}
		}
		if allQueried {
			break
		}
	}

	return uniqueSorted(shortlist, target, K)
}

func uniqueSorted(nodes []Node, target ID, n int) []Node {
	seen := make(map[ID]bool, len(nodes))
	out := nodes[:0]
	for _, nd := range nodes {
		if seen[nd.ID] {
			continue
		}
		seen[nd.ID] = true
		out = append(out, nd)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && CloserTo(target, out[j].ID, out[j-1].ID); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// === Bootstrap ===

func (d *DHT) Bootstrap(seedAddr string) error {
	d.log.Info("bootstrap: pinging seed", "addr", seedAddr)

	resp, err := d.Ping(Node{Addr: seedAddr})
	if err != nil {
		return fmt.Errorf("bootstrap: ping seed: %w", err)
	}
	seed := Node{ID: resp.FromID, Addr: resp.FromAddr}
	seed.Touch()
	d.table.Add(seed)
	d.log.Info("bootstrap: seed identified", "id", seed.ID.String(), "addr", seed.Addr)

	nodes, err := d.FindNodeRPC(seed, d.SelfID)
	if err != nil {
		return fmt.Errorf("bootstrap: find_node: %w", err)
	}
	for _, n := range nodes {
		if n.ID.IsZero() {
			continue
		}
		n.Touch()
		d.table.Add(n)
	}
	d.log.Info("bootstrap: initial nodes", "count", len(nodes))

	for _, n := range nodes {
		if n.ID == d.SelfID || n.ID.IsZero() {
			continue
		}
		go func(n Node) {
			if _, err := d.Ping(n); err != nil {
				d.log.Debug("bootstrap ping failed", "peer", n.Addr, "err", err)
			}
		}(n)
	}

	found := d.FindNode(d.SelfID)
	d.log.Info("bootstrap: self-lookup done", "found", len(found), "table_size", d.table.Size())
	return nil
}

var ErrNoSeed = errors.New("dht: no seed provided")
