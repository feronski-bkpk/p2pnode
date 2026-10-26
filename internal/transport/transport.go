package transport

import (
	"errors"
	"io"
	"time"

	"p2pnode/internal/protocol"
)

type Conn interface {
	ReadFrame() (protocol.Frame, error)

	WriteFrame(protocol.Frame) error

	RemoteAddr() string

	Close() error

	SetDeadline(t time.Time) error

	SetReadDeadline(t time.Time) error

	SetWriteDeadline(t time.Time) error
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
