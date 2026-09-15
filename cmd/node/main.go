package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"p2pnode/internal/config"
	"p2pnode/internal/metrics"
	"p2pnode/internal/node"
	"p2pnode/internal/routing"
	"p2pnode/internal/rpc"
)

func main() {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		os.Exit(2)
	}

	log := newLogger(cfg.LogLevel)

	n, err := node.New(cfg, log)
	if err != nil {
		log.Error("node.New", "err", err)
		os.Exit(1)
	}
	log.Info("listening",
		"addr", n.Listener.Addr(),
		"node_id", n.Local.NodeID.String(),
		"state_dir", cfg.NodeStateDir,
		"k", cfg.KBucketSize,
		"alpha", cfg.Alpha)

	n.Start()

	if err := n.Bootstrap(); err != nil {
		log.Warn("bootstrap error", "err", err)
	}
	log.Info("bootstrap complete", "table_size", n.Table.Size())

	if cfg.ExportDir != "" {
		if err := exportMetrics(cfg, n, log); err != nil {
			log.Error("export metrics", "err", err)
		}
	}

	var wg sync.WaitGroup
	stopCh := make(chan struct{})
	if cfg.ExportDir != "" && cfg.ExportInterval > 0 {
		exp, err := metrics.NewExporter(cfg.ExportDir)
		if err != nil {
			log.Error("metrics.NewExporter", "err", err)
		} else {
			wg.Add(1)
			go func() {
				defer wg.Done()
				runPeriodicExport(stopCh, exp, n, log, cfg.ExportInterval)
			}()
			log.Info("periodic export enabled",
				"interval_ms", cfg.ExportInterval.Milliseconds(),
				"dir", cfg.ExportDir)
		}
	}

	if cfg.DumpRouting {
		log.Info("dump-routing: exiting")
		close(stopCh)
		wg.Wait()
		_ = n.Stop()
		time.Sleep(50 * time.Millisecond)
		return
	}

	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	<-ctx.Done()

	log.Info("shutting down")
	close(stopCh)
	wg.Wait()
	_ = n.Stop()
	time.Sleep(50 * time.Millisecond)
}

func exportMetrics(cfg config.Config, n *node.Node, log *slog.Logger) error {
	exp, err := metrics.NewExporter(cfg.ExportDir)
	if err != nil {
		return err
	}

	path, err := exp.ExportRoutingWithTimestamp(n.Identity.NodeID, n.Table)
	if err != nil {
		log.Error("ExportRouting", "err", err)
	} else {
		log.Info("routing exported", "path", path)
	}

	if cfg.LookupTarget != "" {
		targetID, err := routing.IDFromHex(cfg.LookupTarget)
		if err != nil {
			log.Error("invalid lookup target", "err", err)
			return nil
		}

		if _, present := n.Table.Get(targetID); present {
			n.Table.Remove(targetID)
			log.Info("lookup: removed target from table to satisfy precondition",
				"target", targetID.Short())
		}

		_, presentBefore := n.Table.Get(targetID)
		log.Info("lookup",
			"target", targetID.String(),
			"present_before", presentBefore)

		res := n.Client.LookupNode(
			n.Local,
			n.Table,
			targetID,
			rpc.LookupConfig{
				Alpha:   cfg.Alpha,
				K:       cfg.KBucketSize,
				Timeout: cfg.PingTimeout,
			},
		)
		path, err := exp.ExportLookupWithTimestamp(res, !presentBefore)
		if err != nil {
			log.Error("ExportLookup", "err", err)
		} else {
			log.Info("lookup exported",
				"path", path,
				"rpc", res.RPC,
				"iterations", res.Iterations,
				"timeouts", res.Timeouts,
				"duration_ms", res.Duration.Milliseconds())
		}
	}
	return nil
}

func runPeriodicExport(stop <-chan struct{}, exp *metrics.Exporter,
	n *node.Node, log *slog.Logger, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if _, err := exp.ExportRoutingWithTimestamp(n.Identity.NodeID, n.Table); err != nil {
				log.Warn("periodic export failed", "err", err)
			}
		}
	}
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToUpper(level) {
	case "DEBUG":
		lvl = slog.LevelDebug
	case "WARN":
		lvl = slog.LevelWarn
	case "ERROR":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})
	return slog.New(h)
}
