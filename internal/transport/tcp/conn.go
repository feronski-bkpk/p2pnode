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
		ReadTimeout:    5000 * time.Millisecond,
	}
}

type conn struct {
	nc net.Conn
	r  *bufio.Reader
	w  *bufio.Writer

	wmu sync.Mutex

	rmu         sync.RWMutex
	readTimeout time.Duration
}

func newConn(nc net.Conn, opts Options) *conn {
	return &conn{
		nc:          nc,
		r:           bufio.NewReaderSize(nc, 64*1024),
		w:           bufio.NewWriterSize(nc, 64*1024),
		readTimeout: opts.ReadTimeout,
	}
}

func (c *conn) SetReadTimeout(d time.Duration) {
	c.rmu.Lock()
	c.readTimeout = d
	c.rmu.Unlock()
}

func (c *conn) ReadFrame() (protocol.Frame, error) {
	c.rmu.RLock()
	timeout := c.readTimeout
	c.rmu.RUnlock()

	if timeout > 0 {
		if err := c.nc.SetReadDeadline(time.Now().Add(timeout)); err != nil {
			return protocol.Frame{}, err
		}
	} else {
		_ = c.nc.SetReadDeadline(time.Time{})
	}

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
		return err
	}
	if err := c.w.Flush(); err != nil {
		return err
	}
	return nil
}

func (c *conn) RemoteAddr() string { return c.nc.RemoteAddr().String() }

func (c *conn) Close() error { return c.nc.Close() }
