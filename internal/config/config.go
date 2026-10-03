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

	PublishSelf  bool   `yaml:"publish_self"`
	FindNodeID   string `yaml:"find_node_id"`
	PublishAlias string `yaml:"publish_alias"`
	FindAlias    string `yaml:"find_alias"`

	MaxHops            int `yaml:"max_hops"`
	TunnelPoolSize     int `yaml:"tunnel_pool_size"`
	TunnelAckTimeoutMs int `yaml:"tunnel_ack_timeout_ms"`
	TunnelTTLSec       int `yaml:"tunnel_ttl_sec"`

	SendRepeat     int `yaml:"send_repeat"`
	SendIntervalMs int `yaml:"send_interval_ms"`

	NoServe       bool `yaml:"no_serve"`
	PublishWaitMs int  `yaml:"publish_wait_ms"`

	SendTo   string `yaml:"send_to"`
	SendText string `yaml:"send_text"`

	ExitAfterMs int `yaml:"exit_after_ms"`

	source string
}

func Default() Config {
	return Config{
		NodeStateDir:       "./state",
		ListenHost:         "0.0.0.0",
		ListenPort:         9000,
		BootstrapPeers:     nil,
		SkipSelfLookup:     false,
		NodeIDBits:         256,
		KBucketSize:        4,
		Alpha:              3,
		ConnectTimeout:     3000 * time.Millisecond,
		ReadTimeout:        5000 * time.Millisecond,
		PingTimeout:        5000 * time.Millisecond,
		ConnectTimeoutMs:   3000,
		ReadTimeoutMs:      5000,
		PingTimeoutMs:      5000,
		MaxFramePayload:    65536,
		ProtocolVersion:    1,
		LogLevel:           "INFO",
		ExportDir:          "",
		ExportInterval:     0,
		ExportIntervalMs:   0,
		DumpRouting:        false,
		LookupTarget:       "",
		PublishSelf:        false,
		FindNodeID:         "",
		PublishAlias:       "",
		FindAlias:          "",
		MaxHops:            3,
		TunnelPoolSize:     3,
		TunnelAckTimeoutMs: 5000,
		TunnelTTLSec:       300,
		SendRepeat:         1,
		SendIntervalMs:     2000,
		NoServe:            false,
		PublishWaitMs:      3000,
		SendTo:             "",
		SendText:           "",
		ExitAfterMs:        5000,
		source:             "defaults",
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
	PublishSelf      bool     `yaml:"publish_self"`
	FindNodeID       string   `yaml:"find_node_id"`
	PublishAlias     string   `yaml:"publish_alias"`
	FindAlias        string   `yaml:"find_alias"`

	MaxHops            int  `yaml:"max_hops"`
	TunnelPoolSize     int  `yaml:"tunnel_pool_size"`
	TunnelAckTimeoutMs int  `yaml:"tunnel_ack_timeout_ms"`
	TunnelTTLSec       int  `yaml:"tunnel_ttl_sec"`
	SendRepeat         int  `yaml:"send_repeat"`
	SendIntervalMs     int  `yaml:"send_interval_ms"`
	NoServe            bool `yaml:"no_serve"`
	PublishWaitMs      int  `yaml:"publish_wait_ms"`
	ExitAfterMs        int  `yaml:"exit_after_ms"`

	SendTo   string `yaml:"send_to"`
	SendText string `yaml:"send_text"`
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
	c.PublishSelf = y.PublishSelf
	if y.FindNodeID != "" {
		c.FindNodeID = y.FindNodeID
	}
	if y.PublishAlias != "" {
		c.PublishAlias = y.PublishAlias
	}
	if y.FindAlias != "" {
		c.FindAlias = y.FindAlias
	}
	if y.MaxHops != 0 {
		c.MaxHops = y.MaxHops
	}
	if y.TunnelPoolSize != 0 {
		c.TunnelPoolSize = y.TunnelPoolSize
	}
	if y.TunnelAckTimeoutMs != 0 {
		c.TunnelAckTimeoutMs = y.TunnelAckTimeoutMs
	}
	if y.TunnelTTLSec != 0 {
		c.TunnelTTLSec = y.TunnelTTLSec
	}
	if y.SendRepeat != 0 {
		c.SendRepeat = y.SendRepeat
	}
	if y.SendIntervalMs != 0 {
		c.SendIntervalMs = y.SendIntervalMs
	}
	c.NoServe = y.NoServe
	if y.PublishWaitMs != 0 {
		c.PublishWaitMs = y.PublishWaitMs
	}
	if y.SendTo != "" {
		c.SendTo = y.SendTo
	}
	if y.SendText != "" {
		c.SendText = y.SendText
	}
	if y.ExitAfterMs != 0 {
		c.ExitAfterMs = y.ExitAfterMs
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
		publishSelf    = fs.Bool("publish-self", envBool("PUBLISH_SELF", cfg.PublishSelf), "опубликовать свою NodeRecord в DHT")
		findNodeID     = fs.String("find-node-id", envStr("FIND_NODE_ID", cfg.FindNodeID), "hex NodeID для поиска записи")
		publishAlias   = fs.String("publish-alias", envStr("PUBLISH_ALIAS", cfg.PublishAlias), "псевдоним для публикации")
		findAlias      = fs.String("find-alias", envStr("FIND_ALIAS", cfg.FindAlias), "псевдоним для поиска")

		maxHops        = fs.Int("max-hops", envInt("MAX_HOPS", cfg.MaxHops), "макс. ретрансляторов в туннеле")
		tunnelPoolSize = fs.Int("tunnel-pool-size", envInt("TUNNEL_POOL_SIZE", cfg.TunnelPoolSize), "размер пула туннелей")
		tunnelAckMs    = fs.Int("tunnel-ack-timeout-ms", envInt("TUNNEL_ACK_TIMEOUT_MS", cfg.TunnelAckTimeoutMs), "таймаут TUNNEL_ACK, мс")
		tunnelTTLSec   = fs.Int("tunnel-ttl-sec", envInt("TUNNEL_TTL_SEC", cfg.TunnelTTLSec), "TTL туннеля, сек")
		noServe        = fs.Bool("no-serve", envBool("NO_SERVE", cfg.NoServe), "не поднимать listener (клиент для одной команды)")
		publishWaitMs  = fs.Int("publish-wait-ms", envInt("PUBLISH_WAIT_MS", cfg.PublishWaitMs), "пауза перед публикацией, мс")
		sendRepeat     = fs.Int("send-repeat", envInt("SEND_REPEAT", cfg.SendRepeat), "сколько раз отправить сообщение")
		sendIntervalMs = fs.Int("send-interval-ms", envInt("SEND_INTERVAL_MS", cfg.SendIntervalMs), "интервал между отправками, мс")

		exitAfterMs = fs.Int("exit-after-ms", envInt("EXIT_AFTER_MS", cfg.ExitAfterMs), "форсированный выход для -no-serve, мс")

		sendTo   = fs.String("send-to", envStr("SEND_TO", cfg.SendTo), "hex NodeID получателя")
		sendText = fs.String("send-text", envStr("SEND_TEXT", cfg.SendText), "текст прикладного сообщения")
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
	cfg.PublishSelf = *publishSelf
	cfg.FindNodeID = *findNodeID
	cfg.PublishAlias = *publishAlias
	cfg.FindAlias = *findAlias

	cfg.MaxHops = *maxHops
	cfg.TunnelPoolSize = *tunnelPoolSize
	cfg.TunnelAckTimeoutMs = *tunnelAckMs
	cfg.TunnelTTLSec = *tunnelTTLSec
	cfg.NoServe = *noServe
	cfg.PublishWaitMs = *publishWaitMs
	cfg.SendRepeat = *sendRepeat
	cfg.SendIntervalMs = *sendIntervalMs
	cfg.SendTo = *sendTo
	cfg.SendText = *sendText
	cfg.ExitAfterMs = *exitAfterMs

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
	if c.MaxHops < 2 {
		return fmt.Errorf("config: max_hops must be >= 2, got %d", c.MaxHops)
	}
	if c.TunnelPoolSize < 1 {
		return fmt.Errorf("config: tunnel_pool_size must be >= 1, got %d", c.TunnelPoolSize)
	}
	if c.TunnelTTLSec < 1 {
		return fmt.Errorf("config: tunnel_ttl_sec must be >= 1, got %d", c.TunnelTTLSec)
	}
	if c.PublishWaitMs < 0 {
		return fmt.Errorf("config: publish_wait_ms must be >= 0, got %d", c.PublishWaitMs)
	}
	if c.SendRepeat < 1 {
		return fmt.Errorf("config: send_repeat must be >= 1, got %d", c.SendRepeat)
	}
	if c.SendIntervalMs < 0 {
		return fmt.Errorf("config: send_interval_ms must be >= 0, got %d", c.SendIntervalMs)
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
