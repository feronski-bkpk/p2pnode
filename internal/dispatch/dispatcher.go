package dispatch

import (
	"fmt"
	"log/slog"
	"sync"

	"p2pnode/internal/transport"
)

type Handler = func(c transport.Conn, f transport.Frame) error

type Dispatcher struct {
	mu       sync.RWMutex
	handlers map[transport.MsgType]Handler
	log      *slog.Logger
}

func New(log *slog.Logger) *Dispatcher {
	return &Dispatcher{
		handlers: make(map[transport.MsgType]Handler),
		log:      log,
	}
}

func (d *Dispatcher) Register(t transport.MsgType, h Handler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.handlers[t]; exists {
		panic(fmt.Sprintf("dispatch: handler for %v already registered", t))
	}
	d.handlers[t] = h
}

func (d *Dispatcher) Dispatch(c transport.Conn, f transport.Frame) error {
	d.mu.RLock()
	h, ok := d.handlers[f.Type]
	d.mu.RUnlock()

	if !ok {
		return fmt.Errorf("dispatch: no handler for %v", f.Type)
	}
	return h(c, f)
}

func (d *Dispatcher) Serve(c transport.Conn) {
	defer c.Close()
	for {
		f, err := c.ReadFrame()
		if err != nil {
			d.log.Debug("conn read end", "remote", c.RemoteAddr(), "err", err)
			return
		}
		if err := d.Dispatch(c, f); err != nil {
			d.log.Warn("dispatch error", "remote", c.RemoteAddr(), "type", f.Type, "err", err)
		}
	}
}
