package dht

import "time"

type Node struct {
	ID       ID        `msgpack:"id"`
	Addr     string    `msgpack:"addr"`
	LastSeen time.Time `msgpack:"last_seen"`
}

func (n *Node) Touch() { n.LastSeen = time.Now() }
