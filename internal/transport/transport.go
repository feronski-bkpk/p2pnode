package transport

import (
	"errors"
	"io"

	"p2pnode/internal/protocol"
)

type Conn interface {
	ReadFrame() (protocol.Frame, error)

	WriteFrame(protocol.Frame) error

	RemoteAddr() string

	Close() error
}

type Listener interface {
	Accept() (Conn, error)
	Close() error
	Addr() string
}

type Transport interface {
	Listen(addr string) (Listener, error)
	Dial(addr string) (Conn, error)
}

var ErrClosed = errors.New("transport: closed")

var ErrTimeout = errors.New("transport: timeout")

var _ = io.EOF
