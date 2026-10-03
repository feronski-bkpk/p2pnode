package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	"p2pnode/internal/record"
	"p2pnode/internal/routing"
	"p2pnode/internal/rpc"
	"p2pnode/internal/store"
	"p2pnode/internal/tunnel"
)

func main() {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(2)
	}

	log := newLogger(cfg.LogLevel)

	n, err := node.New(cfg, log)
	if err != nil {
		log.Error("node.New", "err", err)
		os.Exit(1)
	}
	log.Info("config loaded", "source", cfg.Source())
	log.Info("listening",
		"addr", n.Listener.Addr(),
		"node_id", n.Local.NodeID.String(),
		"state_dir", cfg.NodeStateDir,
		"k", cfg.KBucketSize,
		"alpha", cfg.Alpha,
		"no_serve", cfg.NoServe)

	n.OnMessage(func(msg tunnel.Message) {
		log.Info("message received",
			"from", hex.EncodeToString(msg.FromID[:]),
			"from_short", fmt.Sprintf("%x", msg.FromID[:4]),
			"message_id", msg.MessageID.Short(),
			"text", msg.Text,
			"len", len(msg.Text),
		)
	})

	if !cfg.NoServe {
		n.Start()
	}

	if err := n.Bootstrap(); err != nil {
		log.Warn("bootstrap error", "err", err)
	}
	log.Info("bootstrap complete", "table_size", n.Table.Size())

	if !cfg.NoServe {
		n.StartBackgroundTasks()
	}

	if cfg.PublishWaitMs > 0 && (cfg.PublishSelf || cfg.PublishAlias != "") {
		log.Info("publish: waiting for convergence",
			"wait_ms", cfg.PublishWaitMs)
		time.Sleep(time.Duration(cfg.PublishWaitMs) * time.Millisecond)
	}

	if cfg.PublishSelf || cfg.PublishAlias != "" {
		if err := publishRecords(cfg, n, log); err != nil {
			log.Error("publish", "err", err)
		}
	}

	if cfg.FindNodeID != "" {
		if err := findRecordByID(cfg, n, log); err != nil {
			log.Error("find node", "err", err)
		}
	}

	if cfg.FindAlias != "" {
		if err := findRecordByAlias(cfg, n, log); err != nil {
			log.Error("find alias", "err", err)
		}
	}

	if cfg.ExportDir != "" {
		if err := exportMetrics(cfg, n, log); err != nil {
			log.Error("export metrics", "err", err)
		}
	}

	if cfg.SendTo != "" {
		if err := sendMessage(cfg, n, log); err != nil {
			log.Error("send message", "err", err)
		}
	}

	if cfg.NoServe {
		log.Info("no-serve: exiting")

		exitAfter := cfg.ExitAfterMs
		if exitAfter <= 0 {
			exitAfter = 5000
		}
		go func() {
			time.Sleep(time.Duration(exitAfter) * time.Millisecond)
			log.Warn("no-serve: forced exit after timeout", "ms", exitAfter)
			os.Exit(0)
		}()

		_ = n.Stop()
		time.Sleep(50 * time.Millisecond)
		return
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

func sendMessage(cfg config.Config, n *node.Node, log *slog.Logger) error {
	destBytes, err := hex.DecodeString(strings.TrimSpace(cfg.SendTo))
	if err != nil {
		return fmt.Errorf("send-to: hex decode: %w", err)
	}
	if len(destBytes) != 32 {
		return fmt.Errorf("send-to: want 32 bytes, got %d", len(destBytes))
	}
	var destID [32]byte
	copy(destID[:], destBytes)

	log.Info("send: starting",
		"dest", fmt.Sprintf("%x", destID[:4]),
		"text_len", len(cfg.SendText),
		"pool_size", cfg.TunnelPoolSize,
		"repeat", cfg.SendRepeat,
		"interval_ms", cfg.SendIntervalMs)

	if cfg.TunnelPoolSize > 1 {
		start := time.Now()
		if err := n.BuildTunnelPool(destID); err != nil {
			log.Warn("send: pool build failed, fallback to single tunnel",
				"err", err,
				"duration_ms", time.Since(start).Milliseconds())
		} else {
			log.Info("send: pool built",
				"size", cfg.TunnelPoolSize,
				"duration_ms", time.Since(start).Milliseconds())
		}
	}

	repeat := cfg.SendRepeat
	if repeat < 1 {
		repeat = 1
	}
	interval := time.Duration(cfg.SendIntervalMs) * time.Millisecond

	for i := 0; i < repeat; i++ {
		text := cfg.SendText
		if repeat > 1 {
			text = fmt.Sprintf("%s [%d/%d]", cfg.SendText, i+1, repeat)
		}

		log.Info("send: iteration",
			"iteration", i+1,
			"total", repeat,
			"text", text)

		start := time.Now()
		msgID, err := n.SendMessage(destID, text)
		elapsed := time.Since(start)

		if err != nil {
			log.Error("send: failed",
				"iteration", i+1,
				"err", err,
				"duration_ms", elapsed.Milliseconds())
		} else {
			log.Info("send: acked",
				"iteration", i+1,
				"message_id", msgID.Short(),
				"duration_ms", elapsed.Milliseconds())
		}

		st := n.TunnelStats()
		log.Info("tunnel stats",
			"iteration", i+1,
			"sessions", st.Sessions,
			"pools", st.Pools,
			"builds_ok", st.BuildsOK,
			"builds_fail", st.BuildsFail,
			"rebuilds_ok", st.RebuildsOK,
			"rebuilds_fail", st.RebuildsFail,
			"messages", st.Messages)

		if i < repeat-1 && interval > 0 {
			log.Info("send: sleeping between iterations",
				"interval_ms", interval.Milliseconds())
			time.Sleep(interval)
		}
	}

	history := n.History()
	if len(history) > 0 {
		log.Info("history", "count", len(history))
		for _, m := range history {
			direction := "in"
			if m.Direction == tunnel.Outgoing {
				direction = "out"
			}
			log.Info("history entry",
				"direction", direction,
				"message_id", m.MessageID.Short(),
				"from", fmt.Sprintf("%x", m.FromID[:4]),
				"to", fmt.Sprintf("%x", m.ToID[:4]),
				"text", m.Text,
				"delivered", m.Delivered)
		}
	}

	return nil
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

func publishRecords(cfg config.Config, n *node.Node, log *slog.Logger) error {
	addr := n.Listener.Addr()

	rec, err := record.New(n.Identity.PrivateKey, []string{addr}, node.DefaultRecordTTL, 1)
	if err != nil {
		return fmt.Errorf("record.New: %w", err)
	}

	if cfg.PublishAlias != "" {
		rec.Alias = cfg.PublishAlias
		if err := rec.Sign(n.Identity.PrivateKey); err != nil {
			return fmt.Errorf("sign after alias: %w", err)
		}
	}

	result, err := n.Publish(rec, node.DefaultRecordTTL)
	if err != nil {
		return fmt.Errorf("publish: %w", err)
	}

	log.Info("published",
		"node_id", rec.NodeID.Short(),
		"alias", rec.Alias,
		"key", result.Key.Short(),
		"replicas", result.Replicas,
		"local", result.LocalStored,
		"duration_ms", result.Duration.Milliseconds(),
	)
	return nil
}

func findRecordByID(cfg config.Config, n *node.Node, log *slog.Logger) error {
	id, err := record.IDFromHex(cfg.FindNodeID)
	if err != nil {
		return fmt.Errorf("parse find-node-id: %w", err)
	}
	key := node.NodeKeyForID(id)
	return doFindValue(n, key, log, "node_id="+id.Short())
}

func findRecordByAlias(cfg config.Config, n *node.Node, log *slog.Logger) error {
	key := node.AliasKeyForName(cfg.FindAlias)
	return doFindValue(n, key, log, "alias="+cfg.FindAlias)
}

func doFindValue(n *node.Node, key store.ID, log *slog.Logger, label string) error {
	start := time.Now()
	rec, err := n.FindValue(key)
	elapsed := time.Since(start)

	if err != nil {
		log.Warn("findvalue: not found",
			"label", label,
			"key", key.Short(),
			"duration_ms", elapsed.Milliseconds(),
			"err", err)
		return err
	}

	log.Info("findvalue: found",
		"label", label,
		"key", key.Short(),
		"node_id", rec.NodeID.Short(),
		"alias", rec.Alias,
		"addresses", rec.Addresses,
		"seq", rec.SequenceNumber,
		"issued_at", rec.IssuedAt.Format(time.RFC3339),
		"expires_at", rec.ExpiresAt.Format(time.RFC3339),
		"duration_ms", elapsed.Milliseconds(),
	)

	out := map[string]any{
		"node_id":         rec.NodeID.String(),
		"alias":           rec.Alias,
		"addresses":       rec.Addresses,
		"sequence_number": rec.SequenceNumber,
		"issued_at":       rec.IssuedAt.Unix(),
		"expires_at":      rec.ExpiresAt.Unix(),
		"duration_ms":     elapsed.Milliseconds(),
	}
	buf, _ := json.Marshal(out)
	fmt.Println(string(buf))

	return nil
}
