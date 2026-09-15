package config

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	NodeStateDir string

	ListenHost string
	ListenPort int

	BootstrapPeers []string
	SkipSelfLookup bool

	NodeIDBits  int
	KBucketSize int
	Alpha       int

	ConnectTimeout time.Duration
	ReadTimeout    time.Duration
	PingTimeout    time.Duration

	MaxFramePayload int
	ProtocolVersion int

	LogLevel string

	ExportDir      string
	ExportInterval time.Duration
	DumpRouting    bool
	LookupTarget   string
}

func Default() Config {
	return Config{
		NodeStateDir:    "./state",
		ListenHost:      "0.0.0.0",
		ListenPort:      9000,
		BootstrapPeers:  nil,
		SkipSelfLookup:  false,
		NodeIDBits:      256,
		KBucketSize:     4,
		Alpha:           3,
		ConnectTimeout:  3000 * time.Millisecond,
		ReadTimeout:     5000 * time.Millisecond,
		PingTimeout:     5000 * time.Millisecond,
		MaxFramePayload: 65536,
		ProtocolVersion: 1,
		LogLevel:        "INFO",
		ExportDir:       "",
		ExportInterval:  0,
		DumpRouting:     false,
		LookupTarget:    "",
	}
}

func Load(args []string) (Config, error) {
	cfg := Default()

	fs := flag.NewFlagSet("node", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var (
		stateDir       = fs.String("state-dir", envStr("NODE_STATE_DIR", cfg.NodeStateDir), "каталог состояния")
		listenHost     = fs.String("listen-host", envStr("LISTEN_HOST", cfg.ListenHost), "IP для прослушивания")
		listenPort     = fs.Int("listen-port", envInt("LISTEN_PORT", cfg.ListenPort), "TCP-порт")
		bootstrap      = fs.String("bootstrap", envStr("BOOTSTRAP_PEERS", ""), "bootstrap-адреса через запятую (host:port,...)")
		skipSelfLookup = fs.Bool("skip-self-lookup", envBool("SKIP_SELF_LOOKUP", false), "пропустить self-lookup при bootstrap")
		kBucket        = fs.Int("k", envInt("K_BUCKET_SIZE", cfg.KBucketSize), "K_BUCKET_SIZE")
		alpha          = fs.Int("alpha", envInt("ALPHA", cfg.Alpha), "ALPHA (параллелизм lookup)")
		connectMs      = fs.Int("connect-timeout-ms", envInt("CONNECT_TIMEOUT_MS", int(cfg.ConnectTimeout.Milliseconds())), "CONNECT_TIMEOUT_MS")
		readMs         = fs.Int("read-timeout-ms", envInt("READ_TIMEOUT_MS", int(cfg.ReadTimeout.Milliseconds())), "READ_TIMEOUT_MS")
		pingMs         = fs.Int("ping-timeout-ms", envInt("PING_TIMEOUT_MS", int(cfg.PingTimeout.Milliseconds())), "PING_TIMEOUT_MS")
		maxFrame       = fs.Int("max-frame-payload", envInt("MAX_FRAME_PAYLOAD", cfg.MaxFramePayload), "MAX_FRAME_PAYLOAD")
		logLevel       = fs.String("log-level", envStr("LOG_LEVEL", cfg.LogLevel), "DEBUG|INFO|WARN|ERROR")
		exportDir      = fs.String("export-dir", envStr("EXPORT_DIR", ""), "каталог для экспорта JSON-метрик")
		exportMs       = fs.Int("export-interval-ms", envInt("EXPORT_INTERVAL_MS", 0), "интервал выгрузки routing-снапшота (мс), 0 = выключено")
		dumpRouting    = fs.Bool("dump-routing", envBool("DUMP_ROUTING", false), "выгрузить таблицу маршрутизации и выйти")
		lookupTarget   = fs.String("lookup-target", envStr("LOOKUP_TARGET", ""), "hex NodeID для контрольного lookup")
	)

	if err := fs.Parse(args); err != nil {
		return cfg, err
	}

	cfg.NodeStateDir = *stateDir
	cfg.ListenHost = *listenHost
	cfg.ListenPort = *listenPort
	cfg.SkipSelfLookup = *skipSelfLookup
	cfg.KBucketSize = *kBucket
	cfg.Alpha = *alpha
	cfg.ConnectTimeout = time.Duration(*connectMs) * time.Millisecond
	cfg.ReadTimeout = time.Duration(*readMs) * time.Millisecond
	cfg.PingTimeout = time.Duration(*pingMs) * time.Millisecond
	cfg.MaxFramePayload = *maxFrame
	cfg.LogLevel = strings.ToUpper(*logLevel)
	cfg.ExportDir = *exportDir
	cfg.ExportInterval = time.Duration(*exportMs) * time.Millisecond
	cfg.DumpRouting = *dumpRouting
	cfg.LookupTarget = *lookupTarget

	if *bootstrap != "" {
		for _, s := range strings.Split(*bootstrap, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				cfg.BootstrapPeers = append(cfg.BootstrapPeers, s)
			}
		}
	}

	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.ListenPort < 1 || c.ListenPort > 65535 {
		return fmt.Errorf("config: invalid listen_port: %d", c.ListenPort)
	}
	if c.KBucketSize < 1 {
		return fmt.Errorf("config: invalid k_bucket_size: %d", c.KBucketSize)
	}
	if c.Alpha < 1 {
		return fmt.Errorf("config: invalid alpha: %d", c.Alpha)
	}
	if c.MaxFramePayload < 1 {
		return fmt.Errorf("config: invalid max_frame_payload: %d", c.MaxFramePayload)
	}
	if c.NodeIDBits != 256 {
		return fmt.Errorf("config: node_id_bits must be 256, got %d", c.NodeIDBits)
	}
	return nil
}

func (c Config) ListenAddr() string {
	return fmt.Sprintf("%s:%d", c.ListenHost, c.ListenPort)
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}
