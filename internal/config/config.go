package config

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	NodeStateDir string `yaml:"node_state_dir"`

	ListenHost string `yaml:"listen_host"`
	ListenPort int    `yaml:"listen_port"`

	BootstrapPeers []string `yaml:"bootstrap_peers"`
	SkipSelfLookup bool     `yaml:"skip_self_lookup"`

	NodeIDBits  int `yaml:"node_id_bits"`
	KBucketSize int `yaml:"k_bucket_size"`
	Alpha       int `yaml:"alpha"`

	ConnectTimeout time.Duration `yaml:"-"`
	ReadTimeout    time.Duration `yaml:"-"`
	PingTimeout    time.Duration `yaml:"-"`

	ConnectTimeoutMs int `yaml:"connect_timeout_ms"`
	ReadTimeoutMs    int `yaml:"read_timeout_ms"`
	PingTimeoutMs    int `yaml:"ping_timeout_ms"`

	MaxFramePayload int `yaml:"max_frame_payload"`
	ProtocolVersion int `yaml:"protocol_version"`

	LogLevel string `yaml:"log_level"`

	ExportDir        string        `yaml:"export_dir"`
	ExportInterval   time.Duration `yaml:"-"`
	ExportIntervalMs int           `yaml:"export_interval_ms"`
	DumpRouting      bool          `yaml:"dump_routing"`
	LookupTarget     string        `yaml:"lookup_target"`

	source string
}

func Default() Config {
	return Config{
		NodeStateDir:     "./state",
		ListenHost:       "0.0.0.0",
		ListenPort:       9000,
		BootstrapPeers:   nil,
		SkipSelfLookup:   false,
		NodeIDBits:       256,
		KBucketSize:      4,
		Alpha:            3,
		ConnectTimeout:   3000 * time.Millisecond,
		ReadTimeout:      5000 * time.Millisecond,
		PingTimeout:      5000 * time.Millisecond,
		ConnectTimeoutMs: 3000,
		ReadTimeoutMs:    5000,
		PingTimeoutMs:    5000,
		MaxFramePayload:  65536,
		ProtocolVersion:  1,
		LogLevel:         "INFO",
		ExportDir:        "",
		ExportInterval:   0,
		ExportIntervalMs: 0,
		DumpRouting:      false,
		LookupTarget:     "",
		source:           "defaults",
	}
}

type yamlConfig struct {
	NodeStateDir     string   `yaml:"node_state_dir"`
	ListenHost       string   `yaml:"listen_host"`
	ListenPort       int      `yaml:"listen_port"`
	BootstrapPeers   []string `yaml:"bootstrap_peers"`
	SkipSelfLookup   bool     `yaml:"skip_self_lookup"`
	NodeIDBits       int      `yaml:"node_id_bits"`
	KBucketSize      int      `yaml:"k_bucket_size"`
	Alpha            int      `yaml:"alpha"`
	ConnectTimeoutMs int      `yaml:"connect_timeout_ms"`
	ReadTimeoutMs    int      `yaml:"read_timeout_ms"`
	PingTimeoutMs    int      `yaml:"ping_timeout_ms"`
	MaxFramePayload  int      `yaml:"max_frame_payload"`
	ProtocolVersion  int      `yaml:"protocol_version"`
	LogLevel         string   `yaml:"log_level"`
	ExportDir        string   `yaml:"export_dir"`
	ExportIntervalMs int      `yaml:"export_interval_ms"`
	DumpRouting      bool     `yaml:"dump_routing"`
	LookupTarget     string   `yaml:"lookup_target"`
}

func (c *Config) LoadFromFile(path string) error {
	buf, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("config: read %s: %w", path, err)
	}

	var y yamlConfig
	if err := yaml.Unmarshal(buf, &y); err != nil {
		return fmt.Errorf("config: parse %s: %w", path, err)
	}

	if y.NodeStateDir != "" {
		c.NodeStateDir = y.NodeStateDir
	}
	if y.ListenHost != "" {
		c.ListenHost = y.ListenHost
	}
	if y.ListenPort != 0 {
		c.ListenPort = y.ListenPort
	}
	if y.BootstrapPeers != nil {
		c.BootstrapPeers = y.BootstrapPeers
	}
	c.SkipSelfLookup = y.SkipSelfLookup
	if y.NodeIDBits != 0 {
		c.NodeIDBits = y.NodeIDBits
	}
	if y.KBucketSize != 0 {
		c.KBucketSize = y.KBucketSize
	}
	if y.Alpha != 0 {
		c.Alpha = y.Alpha
	}
	if y.ConnectTimeoutMs != 0 {
		c.ConnectTimeout = time.Duration(y.ConnectTimeoutMs) * time.Millisecond
		c.ConnectTimeoutMs = y.ConnectTimeoutMs
	}
	if y.ReadTimeoutMs != 0 {
		c.ReadTimeout = time.Duration(y.ReadTimeoutMs) * time.Millisecond
		c.ReadTimeoutMs = y.ReadTimeoutMs
	}
	if y.PingTimeoutMs != 0 {
		c.PingTimeout = time.Duration(y.PingTimeoutMs) * time.Millisecond
		c.PingTimeoutMs = y.PingTimeoutMs
	}
	if y.MaxFramePayload != 0 {
		c.MaxFramePayload = y.MaxFramePayload
	}
	if y.ProtocolVersion != 0 {
		c.ProtocolVersion = y.ProtocolVersion
	}
	if y.LogLevel != "" {
		c.LogLevel = strings.ToUpper(y.LogLevel)
	}
	if y.ExportDir != "" {
		c.ExportDir = y.ExportDir
	}
	if y.ExportIntervalMs != 0 {
		c.ExportInterval = time.Duration(y.ExportIntervalMs) * time.Millisecond
		c.ExportIntervalMs = y.ExportIntervalMs
	}
	c.DumpRouting = y.DumpRouting
	if y.LookupTarget != "" {
		c.LookupTarget = y.LookupTarget
	}

	c.source = path
	return nil
}

func Load(args []string) (Config, error) {
	cfg := Default()

	preFs := flag.NewFlagSet("pre", flag.ContinueOnError)
	preFs.SetOutput(io.Discard)
	configPath := preFs.String("config", "", "путь к YAML-конфигу")
	_ = preFs.Parse(args)

	if *configPath != "" {
		if err := cfg.LoadFromFile(*configPath); err != nil {
			return cfg, err
		}
	}

	fs := flag.NewFlagSet("node", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var (
		configFlag     = fs.String("config", *configPath, "путь к YAML-конфигу")
		stateDir       = fs.String("state-dir", envStr("NODE_STATE_DIR", cfg.NodeStateDir), "каталог состояния")
		listenHost     = fs.String("listen-host", envStr("LISTEN_HOST", cfg.ListenHost), "IP для прослушивания")
		listenPort     = fs.Int("listen-port", envInt("LISTEN_PORT", cfg.ListenPort), "TCP-порт")
		bootstrap      = fs.String("bootstrap", envStr("BOOTSTRAP_PEERS", strings.Join(cfg.BootstrapPeers, ",")), "bootstrap-адреса через запятую")
		skipSelfLookup = fs.Bool("skip-self-lookup", envBool("SKIP_SELF_LOOKUP", cfg.SkipSelfLookup), "пропустить self-lookup при bootstrap")
		kBucket        = fs.Int("k", envInt("K_BUCKET_SIZE", cfg.KBucketSize), "K_BUCKET_SIZE")
		alpha          = fs.Int("alpha", envInt("ALPHA", cfg.Alpha), "ALPHA")
		connectMs      = fs.Int("connect-timeout-ms", envInt("CONNECT_TIMEOUT_MS", int(cfg.ConnectTimeout.Milliseconds())), "CONNECT_TIMEOUT_MS")
		readMs         = fs.Int("read-timeout-ms", envInt("READ_TIMEOUT_MS", int(cfg.ReadTimeout.Milliseconds())), "READ_TIMEOUT_MS")
		pingMs         = fs.Int("ping-timeout-ms", envInt("PING_TIMEOUT_MS", int(cfg.PingTimeout.Milliseconds())), "PING_TIMEOUT_MS")
		maxFrame       = fs.Int("max-frame-payload", envInt("MAX_FRAME_PAYLOAD", cfg.MaxFramePayload), "MAX_FRAME_PAYLOAD")
		logLevel       = fs.String("log-level", envStr("LOG_LEVEL", cfg.LogLevel), "DEBUG|INFO|WARN|ERROR")
		exportDir      = fs.String("export-dir", envStr("EXPORT_DIR", cfg.ExportDir), "каталог для экспорта JSON-метрик")
		exportMs       = fs.Int("export-interval-ms", envInt("EXPORT_INTERVAL_MS", int(cfg.ExportInterval.Milliseconds())), "интервал выгрузки, мс")
		dumpRouting    = fs.Bool("dump-routing", envBool("DUMP_ROUTING", cfg.DumpRouting), "выгрузить таблицу и выйти")
		lookupTarget   = fs.String("lookup-target", envStr("LOOKUP_TARGET", cfg.LookupTarget), "hex NodeID для контрольного lookup")
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
	cfg.ConnectTimeoutMs = *connectMs
	cfg.ReadTimeout = time.Duration(*readMs) * time.Millisecond
	cfg.ReadTimeoutMs = *readMs
	cfg.PingTimeout = time.Duration(*pingMs) * time.Millisecond
	cfg.PingTimeoutMs = *pingMs
	cfg.MaxFramePayload = *maxFrame
	cfg.LogLevel = strings.ToUpper(*logLevel)
	cfg.ExportDir = *exportDir
	cfg.ExportInterval = time.Duration(*exportMs) * time.Millisecond
	cfg.ExportIntervalMs = *exportMs
	cfg.DumpRouting = *dumpRouting
	cfg.LookupTarget = *lookupTarget

	if *bootstrap != "" {
		cfg.BootstrapPeers = nil
		for _, s := range strings.Split(*bootstrap, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				cfg.BootstrapPeers = append(cfg.BootstrapPeers, s)
			}
		}
	}

	if *configFlag != "" && cfg.source == "defaults" {
		cfg.source = *configFlag
	}

	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (c Config) Source() string {
	if c.source == "" {
		return "defaults"
	}
	return c.source
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
