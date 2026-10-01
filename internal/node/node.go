package node

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"p2pnode/internal/config"
	"p2pnode/internal/events"
	"p2pnode/internal/identity"
	"p2pnode/internal/routing"
	"p2pnode/internal/rpc"
	"p2pnode/internal/store"
	"p2pnode/internal/transport"
	"p2pnode/internal/transport/tcp"
)

type Node struct {
	Config   config.Config
	Log      *slog.Logger
	Identity *identity.Identity
	Local    routing.Contact

	Table    *routing.RoutingTable
	Store    *store.Store
	Client   *rpc.Client
	Server   *rpc.Server
	Checker  *rpc.PingChecker
	Listener transport.Listener

	Events *events.Logger

	ExpireInterval    time.Duration
	RepublishInterval time.Duration

	bgCtx    context.Context
	bgCancel context.CancelFunc
	bgWG     sync.WaitGroup
}

func New(cfg config.Config, log *slog.Logger) (*Node, error) {
	id, err := identity.LoadOrGenerate(cfg.NodeStateDir)
	if err != nil {
		return nil, fmt.Errorf("node: identity: %w", err)
	}

	eventsPath := filepath.Join(cfg.NodeStateDir, "events.jsonl")
	ev, err := events.NewLogger(eventsPath, id.NodeID.Short())
	if err != nil {
		log.Warn("events: logger disabled", "err", err)
		ev, _ = events.NewLogger("", id.NodeID.Short())
	}

	ev.Log("identity_loaded", map[string]any{
		"node_id":   id.NodeID.String(),
		"state_dir": cfg.NodeStateDir,
	})

	tr := tcp.NewWithOptions(tcp.Options{
		ConnectTimeout: cfg.ConnectTimeout,
		ReadTimeout:    cfg.ReadTimeout,
	})

	ln, err := tr.Listen(cfg.ListenAddr())
	if err != nil {
		ev.Close()
		return nil, fmt.Errorf("node: listen %s: %w", cfg.ListenAddr(), err)
	}

	host, port, err := splitListenAddr(ln.Addr())
	if err != nil {
		ln.Close()
		ev.Close()
		return nil, fmt.Errorf("node: parse listen addr: %w", err)
	}
	if host == "0.0.0.0" || host == "::" || host == "" {
		host = "127.0.0.1"
	}

	now := uint64(time.Now().UnixMilli())
	local := routing.Contact{
		NodeID:            id.NodeID,
		IdentityAlgorithm: "ed25519",
		IdentityPublicKey: id.PublicKey,
		Host:              host,
		Port:              port,
		LastSeenMs:        now,
		LastVerifiedMs:    now,
	}

	table := routing.NewRoutingTable(id.NodeID, cfg.KBucketSize)
	table.SetObserver(NewRoutingObserver(ev))

	client := rpc.NewClient(tr, log, ev, id)
	checker := &rpc.PingChecker{
		Client:  client,
		Local:   local,
		Timeout: cfg.PingTimeout,
	}
	st := store.New()
	srv := rpc.NewServer(local, id, table, st, checker, log, ev)

	ev.Log("server_started", map[string]any{
		"addr": ln.Addr(),
	})

	return &Node{
		Config:            cfg,
		Log:               log,
		Identity:          id,
		Local:             local,
		Table:             table,
		Store:             st,
		Client:            client,
		Server:            srv,
		Checker:           checker,
		Listener:          ln,
		Events:            ev,
		ExpireInterval:    ExpireInterval,
		RepublishInterval: RepublishInterval,
	}, nil
}

func (n *Node) Start() {
	go n.Server.Serve(n.Listener)
}

func (n *Node) Stop() error {
	err := n.Listener.Close()
	n.stopBackgroundTasks()
	if n.Events != nil {
		_ = n.Events.Close()
	}
	return err
}

func (n *Node) Bootstrap() error {
	if len(n.Config.BootstrapPeers) == 0 {
		n.Log.Info("bootstrap: skipped (no peers)")
		return nil
	}

	for _, addr := range n.Config.BootstrapPeers {
		n.Log.Info("bootstrap: pinging seed", "addr", addr)
		seed, err := n.Client.Ping(n.Local, addr, nil, n.Config.PingTimeout)
		if err != nil {
			n.Log.Warn("bootstrap: ping seed failed", "addr", addr, "err", err)
			continue
		}
		n.Log.Info("bootstrap: seed identified",
			"node_id", seed.NodeID.Short(),
			"addr", seed.Addr())

		if n.Config.SkipSelfLookup {
			nodes, err := n.Client.FindNodeRPC(n.Local, seed, n.Identity.NodeID, n.Config.PingTimeout)
			if err != nil {
				n.Log.Warn("bootstrap: find_node from seed failed", "err", err)
				continue
			}
			added := 0
			for _, c := range nodes {
				if c.NodeID == n.Identity.NodeID {
					continue
				}
				if n.Table.Add(c, n.Checker) {
					added++
				}
			}
			n.Log.Info("bootstrap: skip-self-lookup done",
				"received", len(nodes),
				"added", added,
				"table_size", n.Table.Size())
			return nil
		}

		n.Table.Add(seed, n.Checker)

		result := n.Client.LookupNode(
			n.Local,
			n.Table,
			n.Identity.NodeID,
			rpc.LookupConfig{
				Alpha:   n.Config.Alpha,
				K:       n.Config.KBucketSize,
				Timeout: n.Config.PingTimeout,
			},
		)
		n.Log.Info("bootstrap: self-lookup done",
			"rpc", result.RPC,
			"iterations", result.Iterations,
			"duration_ms", result.Duration.Milliseconds(),
			"table_size", n.Table.Size())
	}
	return nil
}

func (n *Node) SnapshotContacts() []routing.Contact {
	return n.Table.Snapshot()
}

func splitListenAddr(addr string) (string, uint16, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, err
	}
	p, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, err
	}
	return host, uint16(p), nil
}
