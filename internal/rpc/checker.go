package rpc

import (
	"time"

	"p2pnode/internal/routing"
)

type PingChecker struct {
	Client  *Client
	Local   routing.Contact
	Timeout time.Duration
}

func (c *PingChecker) IsAlive(ct routing.Contact) bool {
	expected := ct.NodeID
	_, err := c.Client.Ping(c.Local, ct.Addr(), &expected, c.Timeout)
	return err == nil
}
