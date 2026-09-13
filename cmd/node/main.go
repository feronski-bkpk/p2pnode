package main

import (
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"p2pnode/internal/dispatch"
	"p2pnode/internal/transport"
	"p2pnode/internal/transport/tcp"
)

func main() {
	var (
		listenAddr = flag.String("listen", ":9000", "адрес для входящих соединений")
		connectTo  = flag.String("connect", "", "адрес узла для подключения (host:port)")
		message    = flag.String("msg", "", "текстовое сообщение для отправки")
		logLevel   = flag.String("log", "info", "уровень логов: debug|info|warn|error")
	)
	flag.Parse()

	log := newLogger(*logLevel)

	tr := tcp.New()
	disp := dispatch.New(log)
	registerEchoHandlers(disp, log)

	// Слушаем входящие — всегда.
	ln, err := tr.Listen(*listenAddr)
	if err != nil {
		log.Error("listen failed", "addr", *listenAddr, "err", err)
		os.Exit(1)
	}
	log.Info("listening", "addr", ln.Addr())

	// Принимаем соединения.
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				log.Debug("accept end", "err", err)
				return
			}
			log.Info("incoming connection", "remote", c.RemoteAddr())
			go disp.Serve(c)
		}
	}()

	// Исходящее соединение — если попросили.
	if *connectTo != "" {
		c, err := tr.Dial(*connectTo)
		if err != nil {
			log.Error("dial failed", "addr", *connectTo, "err", err)
			os.Exit(1)
		}
		log.Info("connected", "remote", c.RemoteAddr())

		if *message != "" {
			f := transport.Frame{
				Type:    transport.MsgText,
				Payload: []byte(*message),
			}
			if err := c.WriteFrame(f); err != nil {
				log.Error("write failed", "err", err)
				os.Exit(1)
			}
			log.Info("sent", "bytes", len(f.Payload))
		}

		// Читаем входящие на этом соединении тоже.
		go disp.Serve(c)
	}

	// Ждём Ctrl+C.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Info("shutting down")
	_ = ln.Close()
}

func registerEchoHandlers(d *dispatch.Dispatcher, log *slog.Logger) {
	// PING → PONG
	d.Register(transport.MsgPing, func(c transport.Conn, f transport.Frame) error {
		return c.WriteFrame(transport.Frame{Type: transport.MsgPong})
	})
	// PONG — просто лог
	d.Register(transport.MsgPong, func(c transport.Conn, f transport.Frame) error {
		log.Info("pong", "remote", c.RemoteAddr())
		return nil
	})
	// TEXT — печатаем
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
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})
	return slog.New(h)
}
