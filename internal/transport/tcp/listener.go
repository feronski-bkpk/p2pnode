package tcp

import (
	"net"

	"p2pnode/internal/transport"
)

type listener struct {
	ln   net.Listener
	opts Options
}

func (l *listener) Accept() (transport.Conn, error) {
	nc, err := l.ln.Accept()
	if err != nil {
		return nil, err
	}
	if tc, ok := nc.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
	}
	return newConn(nc, l.opts), nil
}

func (l *listener) Close() error { return l.ln.Close() }
func (l *listener) Addr() string { return l.ln.Addr().String() }
