package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"p2pnode/internal/dht"
	"p2pnode/internal/dispatch"
	"p2pnode/internal/transport"
	"p2pnode/internal/transport/tcp"
)

func main() {
	var (
		listenAddr = flag.String("listen", ":9000", "адрес для входящих соединений")
		bootstrap  = flag.String("bootstrap", "", "адрес seed-узла (host:port)")
		connectTo  = flag.String("connect", "", "разовое подключение для теста (host:port)")
		message    = flag.String("msg", "", "сообщение для отправки (тест транспорта)")
		logLevel   = flag.String("log", "info", "debug|info|warn|error")
	)
	flag.Parse()

	log := newLogger(*logLevel)
	tr := tcp.New()

	selfID, err := dht.RandomID()
	if err != nil {
		log.Error("random id", "err", err)
		os.Exit(1)
	}

	ln, err := tr.Listen(*listenAddr)
	if err != nil {
		log.Error("listen", "addr", *listenAddr, "err", err)
		os.Exit(1)
	}
	selfAddr := ln.Addr()
	log.Info("listening", "addr", selfAddr, "id", selfID.String())

	d := dht.New(selfID, selfAddr, tr, log)

	disp := dispatch.New(log)
	d.RegisterAll(disp)
	registerEchoHandlers(disp, log)

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				log.Debug("accept end", "err", err)
				return
			}
			go disp.Serve(c)
		}
	}()

	if *bootstrap != "" {
		if err := d.Bootstrap(*bootstrap); err != nil {
			log.Error("bootstrap failed", "seed", *bootstrap, "err", err)
		} else {
			log.Info("bootstrap ok", "table_size", d.Table().Size())
			for _, n := range d.Table().Snapshot() {
				log.Info("known peer", "id", n.ID.String(), "addr", n.Addr)
			}
		}
	}

	if *connectTo != "" {
		c, err := tr.Dial(*connectTo)
		if err != nil {
			log.Error("dial", "addr", *connectTo, "err", err)
			os.Exit(1)
		}
		defer c.Close()
		if *message != "" {
			if err := c.WriteFrame(transport.Frame{
				Type:    transport.MsgText,
				Payload: []byte(*message),
			}); err != nil {
				log.Error("write", "err", err)
				os.Exit(1)
			}
			log.Info("sent", "bytes", len(*message))
		}
		go disp.Serve(c)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Info("shutting down")
	_ = ln.Close()
}

func registerEchoHandlers(d *dispatch.Dispatcher, log *slog.Logger) {
	d.Register(transport.MsgText, func(c transport.Conn, f transport.Frame) error {
		log.Info("text received", "remote", c.RemoteAddr(), "body", string(f.Payload))
		return nil
	})
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}

var _ = fmt.Sprintf
