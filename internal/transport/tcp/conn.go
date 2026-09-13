package tcp

import (
	"bufio"
	"net"
	"sync"
	"time"

	"p2pnode/internal/transport"
)

// conn — реализация transport.Conn поверх net.Conn.
// Один writer-guard: WriteFrame не должен вызываться из двух горутин одновременно.
type conn struct {
	nc net.Conn
	r  *bufio.Reader // буферизуем чтение — меньше syscalls на заголовках
	w  *bufio.Writer

	wmu sync.Mutex // сериализует WriteFrame

	// таймауты на кадр; 0 = без таймаута
	readTimeout time.Duration
}

func newConn(nc net.Conn) *conn {
	return &conn{
		nc:          nc,
		r:           bufio.NewReaderSize(nc, 64*1024),
		w:           bufio.NewWriterSize(nc, 64*1024),
		readTimeout: 30 * time.Second,
	}
}

func (c *conn) ReadFrame() (transport.Frame, error) {
	if c.readTimeout > 0 {
		_ = c.nc.SetReadDeadline(time.Now().Add(c.readTimeout))
	}
	return ReadFrame(c.r)
}

func (c *conn) WriteFrame(f transport.Frame) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()

	if err := WriteFrame(c.w, f); err != nil {
		return err
	}
	return c.w.Flush()
}

func (c *conn) RemoteAddr() string { return c.nc.RemoteAddr().String() }
func (c *conn) Close() error       { return c.nc.Close() }
