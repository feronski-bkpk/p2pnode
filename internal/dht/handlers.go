package dht

import (
	"p2pnode/internal/transport"
)

func (d *DHT) HandlePing(c transport.Conn, f transport.Frame) error {
	var req PingRequest
	if err := Decode(f.Payload, &req); err != nil {
		return err
	}
	n := Node{ID: req.FromID, Addr: req.FromAddr}
	n.Touch()
	d.table.Add(n)

	resp, err := Encode(PingResponse{FromID: d.SelfID, FromAddr: d.SelfAddr})
	if err != nil {
		return err
	}
	return c.WriteFrame(transport.Frame{Type: transport.MsgPong, Payload: resp})
}

func (d *DHT) HandleFindNode(c transport.Conn, f transport.Frame) error {
	var req FindNodeRequest
	if err := Decode(f.Payload, &req); err != nil {
		return err
	}
	n := Node{ID: req.FromID, Addr: req.FromAddr}
	n.Touch()
	d.table.Add(n)

	closest := d.table.Closest(req.Target, K)
	resp, err := Encode(FindNodeResponse{FromID: d.SelfID, Nodes: closest})
	if err != nil {
		return err
	}
	return c.WriteFrame(transport.Frame{Type: transport.MsgFindNode, Payload: resp})
}

type Registrar interface {
	Register(t transport.MsgType, h func(c transport.Conn, f transport.Frame) error)
}

func (d *DHT) RegisterAll(r Registrar) {
	r.Register(transport.MsgPing, d.HandlePing)
	r.Register(transport.MsgFindNode, d.HandleFindNode)
}
