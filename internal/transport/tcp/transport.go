package tcp

import (
	"net"
	"time"

	"p2pnode/internal/transport"
)

type Transport struct{}

func New() *Transport { return &Transport{} }

func (Transport) Listen(addr string) (transport.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &listener{ln: ln}, nil
}

func (Transport) Dial(addr string) (transport.Conn, error) {
	nc, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, err
	}
	if tc, ok := nc.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
	}
	return newConn(nc), nil
}
