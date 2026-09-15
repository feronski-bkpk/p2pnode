package tcp

import (
	"net"
	"time"

	"p2pnode/internal/transport"
)

type Transport struct {
	opts Options
}

func New() *Transport {
	return &Transport{opts: DefaultOptions()}
}

func NewWithOptions(opts Options) *Transport {
	return &Transport{opts: opts}
}

func (t *Transport) Listen(addr string) (transport.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &listener{ln: ln, opts: t.opts}, nil
}

func (t *Transport) Dial(addr string) (transport.Conn, error) {
	timeout := t.opts.ConnectTimeout
	if timeout <= 0 {
		timeout = 3000 * time.Millisecond
	}
	nc, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, err
	}
	if tc, ok := nc.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
	}
	return newConn(nc, t.opts), nil
}
