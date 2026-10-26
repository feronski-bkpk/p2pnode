package tcp

import (
	"bufio"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"p2pnode/internal/protocol"
	"p2pnode/internal/transport"
)

type Options struct {
	ConnectTimeout time.Duration
	ReadTimeout    time.Duration
}

func DefaultOptions() Options {
	return Options{
		ConnectTimeout: 3000 * time.Millisecond,
		ReadTimeout:    0,
	}
}

type conn struct {
	nc net.Conn
	r  *bufio.Reader
	w  *bufio.Writer

	wmu sync.Mutex
}

func newConn(nc net.Conn, opts Options) *conn {
	return &conn{
		nc: nc,
		r:  bufio.NewReaderSize(nc, 64*1024),
		w:  bufio.NewWriterSize(nc, 64*1024),
	}
}

func (c *conn) ReadFrame() (protocol.Frame, error) {
	f, err := protocol.ReadFrame(c.r)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return protocol.Frame{}, io.EOF
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return protocol.Frame{}, transport.ErrTimeout
		}
		return protocol.Frame{}, err
	}
	return f, nil
}

func (c *conn) WriteFrame(f protocol.Frame) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()

	if _, err := f.WriteTo(c.w); err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return transport.ErrTimeout
		}
		return err
	}
	if err := c.w.Flush(); err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return transport.ErrTimeout
		}
		return err
	}
	return nil
}

func (c *conn) RemoteAddr() string { return c.nc.RemoteAddr().String() }

func (c *conn) Close() error { return c.nc.Close() }

func (c *conn) SetDeadline(t time.Time) error {
	return c.nc.SetDeadline(t)
}

func (c *conn) SetReadDeadline(t time.Time) error {
	return c.nc.SetReadDeadline(t)
}

func (c *conn) SetWriteDeadline(t time.Time) error {
	return c.nc.SetWriteDeadline(t)
}
