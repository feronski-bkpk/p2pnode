package tunnel

import (
	"crypto/ed25519"
	"sync"
	"time"
)

type HopType int

const (
	HopInitiator HopType = iota
	HopRelay
	HopDest
)

func (h HopType) String() string {
	switch h {
	case HopInitiator:
		return "initiator"
	case HopRelay:
		return "relay"
	case HopDest:
		return "dest"
	default:
		return "unknown"
	}
}

type Hop struct {
	NodeID [32]byte
	Addr   string
	Type   HopType
}

type Config struct {
	MaxHops uint8

	BuildTimeout time.Duration

	AckTimeout time.Duration

	IdleTimeout time.Duration

	MaxRetries int

	TTL time.Duration
}

func DefaultConfig() Config {
	return Config{
		MaxHops:      3,
		BuildTimeout: 15 * time.Second,
		AckTimeout:   5 * time.Second,
		IdleTimeout:  5 * time.Minute,
		MaxRetries:   3,
		TTL:          5 * time.Minute,
	}
}

type E2ESession struct {
	SendKey []byte
	RecvKey []byte

	SendCounter uint64
	RecvCounter uint64

	mu sync.Mutex
}

type Tunnel struct {
	ID ID

	Initiator Hop
	Dest      Hop
	Path      []Hop

	E2E *E2ESession

	state   State
	stateMu sync.RWMutex

	createdAt   time.Time
	activatedAt time.Time
	expiresAt   time.Time
	lastUseAt   time.Time
	useMu       sync.Mutex

	messagesSent uint64
	messagesRecv uint64
	retries      int

	cfg Config
}

func NewTunnel(id ID, initiator, dest Hop, path []Hop, cfg Config) *Tunnel {
	if cfg.TTL == 0 {
		cfg.TTL = DefaultConfig().TTL
	}
	now := time.Now()
	return &Tunnel{
		ID:        id,
		Initiator: initiator,
		Dest:      dest,
		Path:      path,
		state:     StateBuilding,
		createdAt: now,
		expiresAt: now.Add(cfg.TTL),
		cfg:       cfg,
	}
}

func (t *Tunnel) State() State {
	t.stateMu.RLock()
	defer t.stateMu.RUnlock()
	return t.state
}

func (t *Tunnel) SetState(s State) {
	t.stateMu.Lock()
	defer t.stateMu.Unlock()
	if t.state == StateDead {
		return
	}
	t.state = s
	if s == StateActive && t.activatedAt.IsZero() {
		t.activatedAt = time.Now()
	}
}

func (t *Tunnel) Activate(e2e *E2ESession) {
	t.stateMu.Lock()
	defer t.stateMu.Unlock()
	t.E2E = e2e
	t.state = StateActive
	if t.activatedAt.IsZero() {
		t.activatedAt = time.Now()
	}
	t.lastUseAt = time.Now()
}

func (t *Tunnel) MarkUsed() {
	t.useMu.Lock()
	defer t.useMu.Unlock()
	t.lastUseAt = time.Now()
}

func (t *Tunnel) LastUseAt() time.Time {
	t.useMu.Lock()
	defer t.useMu.Unlock()
	return t.lastUseAt
}

func (t *Tunnel) ExpiresAt() time.Time {
	return t.expiresAt
}

func (t *Tunnel) Expired() bool {
	return time.Now().After(t.expiresAt)
}

func (t *Tunnel) TTLLeft() time.Duration {
	d := time.Until(t.expiresAt)
	if d < 0 {
		return 0
	}
	return d
}

func (t *Tunnel) RefreshTTL() {
	t.expiresAt = time.Now().Add(t.cfg.TTL)
}

func (t *Tunnel) NumRelays() int {
	if len(t.Path) < 2 {
		return 0
	}
	return len(t.Path) - 2
}

func (t *Tunnel) IncSent() {
	t.useMu.Lock()
	defer t.useMu.Unlock()
	t.messagesSent++
}

func (t *Tunnel) IncRecv() {
	t.useMu.Lock()
	defer t.useMu.Unlock()
	t.messagesRecv++
}

func (t *Tunnel) Stats() TunnelStats {
	t.useMu.Lock()
	defer t.useMu.Unlock()
	return TunnelStats{
		ID:           t.ID,
		State:        t.State(),
		NumRelays:    t.NumRelays(),
		MessagesSent: t.messagesSent,
		MessagesRecv: t.messagesRecv,
		Retries:      t.retries,
		CreatedAt:    t.createdAt,
		ActivatedAt:  t.activatedAt,
		ExpiresAt:    t.expiresAt,
		LastUseAt:    t.lastUseAt,
	}
}

type TunnelStats struct {
	ID           ID        `json:"id"`
	State        State     `json:"state"`
	NumRelays    int       `json:"num_relays"`
	MessagesSent uint64    `json:"messages_sent"`
	MessagesRecv uint64    `json:"messages_recv"`
	Retries      int       `json:"retries"`
	CreatedAt    time.Time `json:"created_at"`
	ActivatedAt  time.Time `json:"activated_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	LastUseAt    time.Time `json:"last_use_at"`
}

func (e *E2ESession) NextSendNonce() uint64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := e.SendCounter
	e.SendCounter++
	return n
}

func (e *E2ESession) CheckRecvNonce(counter uint64) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if counter < e.RecvCounter {
		return ErrReplay
	}
	if counter > e.RecvCounter {
		return ErrNonceGap
	}
	e.RecvCounter++
	return nil
}

var (
	ErrReplay = errTunnel("replay detected")

	ErrNonceGap = errTunnel("nonce gap")

	ErrTunnelClosed = errTunnel("tunnel closed")

	ErrTunnelNotActive = errTunnel("tunnel not active")

	ErrTunnelNotFound = errTunnel("tunnel not found")

	ErrBuildFailed = errTunnel("tunnel build failed")

	ErrNoCandidates = errTunnel("no candidates for relay")

	ErrTunnelExpired = errTunnel("tunnel expired")
)

type errTunnel string

func (e errTunnel) Error() string { return "tunnel: " + string(e) }

func SignTranscript(priv ed25519.PrivateKey, transcript []byte) []byte {
	return ed25519.Sign(priv, transcript)
}

func VerifyTranscript(pub ed25519.PublicKey, transcript, sig []byte) bool {
	return ed25519.Verify(pub, transcript, sig)
}
