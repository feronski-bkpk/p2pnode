package tunnel

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type Manager struct {
	mu      sync.RWMutex
	tunnels map[ID]*Tunnel
}

func NewManager() *Manager {
	return &Manager{tunnels: make(map[ID]*Tunnel)}
}

func (m *Manager) Add(t *Tunnel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tunnels[t.ID] = t
}

func (m *Manager) Get(id ID) (*Tunnel, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tunnels[id]
	return t, ok
}

func (m *Manager) Remove(id ID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tunnels, id)
}

func (m *Manager) All() []*Tunnel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Tunnel, 0, len(m.tunnels))
	for _, t := range m.tunnels {
		out = append(out, t)
	}
	return out
}

func (m *Manager) Active(destID [32]byte) []*Tunnel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Tunnel
	for _, t := range m.tunnels {
		if t.Dest.NodeID == destID && t.State().CanSend() {
			out = append(out, t)
		}
	}
	return out
}

func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.tunnels)
}

func (m *Manager) CleanupDead() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	removed := 0
	for id, t := range m.tunnels {
		if t.State().IsTerminal() {
			delete(m.tunnels, id)
			removed++
		}
	}
	return removed
}

func (m *Manager) Stats() []TunnelStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]TunnelStats, 0, len(m.tunnels))
	for _, t := range m.tunnels {
		out = append(out, t.Stats())
	}
	return out
}

type TunnelManager struct {
	LocalID      [32]byte
	LocalAddr    string
	LocalPubKey  ed25519.PublicKey
	LocalPrivKey ed25519.PrivateKey

	Dial     func(addr string) (RawConn, error)
	FindDest func(destID [32]byte) (ed25519.PublicKey, string, error)

	RouteBuilder *RouteBuilder
	ProfileStore *ProfileStore
	Log          *slog.Logger

	PoolSize     int
	AckTimeout   time.Duration
	BuildTimeout time.Duration
	TTL          time.Duration

	mu          sync.RWMutex
	sessions    map[ID]*Session
	pools       map[[32]byte][]*Session
	lastSession map[[32]byte]*Session

	Messages  *MessageStore
	onMessage func(Message)

	statsMu      sync.Mutex
	buildsOK     uint64
	buildsFail   uint64
	rebuildsOK   uint64
	rebuildsFail uint64
}

func NewTunnelManager(
	localID [32]byte,
	localAddr string,
	localPub ed25519.PublicKey,
	localPriv ed25519.PrivateKey,
	dial func(string) (RawConn, error),
	findDest func([32]byte) (ed25519.PublicKey, string, error),
	rb *RouteBuilder,
	profiles *ProfileStore,
	log *slog.Logger,
) *TunnelManager {
	if log == nil {
		log = slog.Default()
	}
	return &TunnelManager{
		LocalID:      localID,
		LocalAddr:    localAddr,
		LocalPubKey:  localPub,
		LocalPrivKey: localPriv,
		Dial:         dial,
		FindDest:     findDest,
		RouteBuilder: rb,
		ProfileStore: profiles,
		Log:          log,
		PoolSize:     3,
		AckTimeout:   5 * time.Second,
		BuildTimeout: 15 * time.Second,
		TTL:          5 * time.Minute,
		sessions:     make(map[ID]*Session),
		pools:        make(map[[32]byte][]*Session),
		lastSession:  make(map[[32]byte]*Session),
		Messages:     NewMessageStore(DefaultMessageStoreSize),
	}
}

func (m *TunnelManager) SetOnMessage(cb func(Message)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onMessage = cb
}

func (m *TunnelManager) Build(destID [32]byte) (*Session, error) {
	sess, err := m.buildInternal(destID, nil)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.lastSession[destID] = sess
	m.mu.Unlock()
	return sess, nil
}

func (m *TunnelManager) buildInternal(destID [32]byte, exclude [][32]byte) (*Session, error) {
	if m.RouteBuilder != nil && m.RouteBuilder.Log == nil {
		m.RouteBuilder.Log = m.Log
	}

	path, err := m.RouteBuilder.Build(destID, exclude)
	if err != nil {
		m.statsMu.Lock()
		m.buildsFail++
		m.statsMu.Unlock()
		return nil, fmt.Errorf("tunnel manager: build route: %w", err)
	}

	m.logPath(destID, path)

	bc := &BuildCoordinator{
		LocalID:      m.LocalID,
		LocalAddr:    m.LocalAddr,
		LocalPubKey:  m.LocalPubKey,
		LocalPrivKey: m.LocalPrivKey,
		Dial:         m.Dial,
		FindDest:     m.FindDest,
		BuildConfig: BuildConfig{
			BuildTimeout: m.BuildTimeout,
			MaxHops:      uint8(m.RouteBuilder.MaxHops),
			TTL:          m.TTL,
		},
		Log: m.Log,
	}

	res, err := bc.Build(destID, path)
	if err != nil {
		m.statsMu.Lock()
		m.buildsFail++
		m.statsMu.Unlock()
		return nil, fmt.Errorf("tunnel manager: build: %w", err)
	}

	sess := NewSession(res.Tunnel, res.Conn, res.E2E, m.Log)
	sess.SetOnMessage(func(msg Message) {
		if !m.Messages.Add(msg) {
			m.Log.Debug("tunnel: duplicate message ignored", "id", msg.MessageID.Short())
			return
		}
		m.mu.RLock()
		cb := m.onMessage
		m.mu.RUnlock()
		if cb != nil {
			cb(msg)
		}
	})
	sess.SetOnClosed(func(s *Session) {
		m.removeSession(s)
	})
	sess.Start()

	m.mu.Lock()
	m.sessions[res.Tunnel.ID] = sess
	m.statsMu.Lock()
	m.buildsOK++
	m.statsMu.Unlock()
	m.mu.Unlock()

	m.Log.Info("tunnel: built",
		"tunnel_id", res.Tunnel.ID.Short(),
		"dest", fmt.Sprintf("%x", destID[:4]),
		"hops", res.Tunnel.NumRelays())

	return sess, nil
}

func (m *TunnelManager) BuildPool(destID [32]byte) ([]*Session, error) {
	sessions := make([]*Session, 0, m.PoolSize)
	var exclude [][32]byte
	var lastErr error

	for i := 0; i < m.PoolSize; i++ {
		s, err := m.buildInternal(destID, exclude)
		if err != nil {
			m.Log.Info("tunnel: pool build without exclude (retry)",
				"dest", fmt.Sprintf("%x", destID[:4]),
				"attempt", i,
				"excluded", len(exclude),
				"err", err)
			s, err = m.buildInternal(destID, nil)
			if err != nil {
				lastErr = err
				m.Log.Warn("tunnel: pool build partial",
					"dest", fmt.Sprintf("%x", destID[:4]),
					"got", len(sessions),
					"want", m.PoolSize,
					"err", err)
				continue
			}
		}
		sessions = append(sessions, s)

		for _, h := range s.Tunnel.Path {
			if h.Type == HopRelay {
				exclude = append(exclude, h.NodeID)
			}
		}
	}

	if len(sessions) == 0 {
		return nil, fmt.Errorf("tunnel manager: pool empty: %w", lastErr)
	}

	m.mu.Lock()
	m.pools[destID] = sessions
	if len(sessions) > 0 {
		m.lastSession[destID] = sessions[0]
	}
	m.mu.Unlock()

	m.Log.Info("tunnel: pool built",
		"dest", fmt.Sprintf("%x", destID[:4]),
		"size", len(sessions),
		"excluded", len(exclude))
	return sessions, nil
}

func (m *TunnelManager) GetOrBuild(destID [32]byte) (*Session, error) {
	m.mu.RLock()
	last := m.lastSession[destID]
	pool := m.pools[destID]
	m.mu.RUnlock()

	if last != nil && last.Tunnel.State().CanSend() && !last.IsClosed() {
		return last, nil
	}
	for _, s := range pool {
		if s.Tunnel.State().CanSend() && !s.IsClosed() {
			return s, nil
		}
	}
	return m.Build(destID)
}

func (m *TunnelManager) SendMessage(destID [32]byte, text string) (MessageID, error) {
	msgID, err := NewMessageID()
	if err != nil {
		return MessageID{}, err
	}

	const maxAttempts = 3
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		sess, err := m.pickSession(destID, attempt)
		if err != nil {
			lastErr = err
			m.Log.Warn("tunnel: pick session failed",
				"dest", fmt.Sprintf("%x", destID[:4]),
				"attempt", attempt,
				"err", err)
			continue
		}

		err = sess.SendMessageWithID(msgID, text, m.AckTimeout)
		if err == nil {
			m.Messages.Add(Message{
				MessageID: msgID,
				FromID:    m.LocalID,
				ToID:      destID,
				Text:      text,
				Timestamp: time.Now(),
				Delivered: true,
				Direction: Outgoing,
			})
			if attempt > 0 {
				m.statsMu.Lock()
				m.rebuildsOK++
				m.statsMu.Unlock()
			}
			return msgID, nil
		}

		lastErr = err
		m.Log.Warn("tunnel: send attempt failed",
			"dest", fmt.Sprintf("%x", destID[:4]),
			"attempt", attempt,
			"msg_id", msgID.Short(),
			"err", err)

		sess.Close()
		m.removeSession(sess)
	}

	m.statsMu.Lock()
	m.rebuildsFail++
	m.statsMu.Unlock()

	m.Messages.Add(Message{
		MessageID: msgID,
		FromID:    m.LocalID,
		ToID:      destID,
		Text:      text,
		Timestamp: time.Now(),
		Delivered: false,
		Direction: Outgoing,
	})

	return msgID, fmt.Errorf("tunnel: send failed after %d attempts: %w", maxAttempts, lastErr)
}

func (m *TunnelManager) pickSession(destID [32]byte, attempt int) (*Session, error) {
	m.mu.RLock()
	pool := m.pools[destID]
	last := m.lastSession[destID]
	m.mu.RUnlock()

	if len(pool) > 0 {
		alive := make([]*Session, 0, len(pool))
		for _, s := range pool {
			if s.Tunnel.State().CanSend() && !s.IsClosed() {
				alive = append(alive, s)
			}
		}
		if attempt < len(alive) {
			return alive[attempt], nil
		}
		return m.Build(destID)
	}

	if attempt == 0 && last != nil &&
		last.Tunnel.State().CanSend() && !last.IsClosed() {
		return last, nil
	}
	return m.Build(destID)
}

func (m *TunnelManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		s.Close()
	}
	m.sessions = make(map[ID]*Session)
	m.pools = make(map[[32]byte][]*Session)
	m.lastSession = make(map[[32]byte]*Session)
}

func (m *TunnelManager) SessionByID(id ID) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	return s, ok
}

func (m *TunnelManager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

type ManagerStats struct {
	Sessions     int    `json:"sessions"`
	Pools        int    `json:"pools"`
	BuildsOK     uint64 `json:"builds_ok"`
	BuildsFail   uint64 `json:"builds_fail"`
	RebuildsOK   uint64 `json:"rebuilds_ok"`
	RebuildsFail uint64 `json:"rebuilds_fail"`
	Messages     int    `json:"messages"`
}

func (m *TunnelManager) Stats() ManagerStats {
	m.mu.RLock()
	sessions := len(m.sessions)
	pools := len(m.pools)
	m.mu.RUnlock()

	m.statsMu.Lock()
	defer m.statsMu.Unlock()
	return ManagerStats{
		Sessions:     sessions,
		Pools:        pools,
		BuildsOK:     m.buildsOK,
		BuildsFail:   m.buildsFail,
		RebuildsOK:   m.rebuildsOK,
		RebuildsFail: m.rebuildsFail,
		Messages:     m.Messages.Count(),
	}
}

func (m *TunnelManager) logPath(destID [32]byte, path []Hop) {
	ids := make([]string, 0, len(path))
	for _, h := range path {
		ids = append(ids, fmt.Sprintf("%x(%s)", h.NodeID[:4], h.Type))
	}
	m.Log.Info("tunnel: route selected",
		"dest", fmt.Sprintf("%x", destID[:4]),
		"hops", len(path)-2,
		"path", ids)

	snap := m.ProfileStore.Snapshot()
	for _, p := range snap {
		m.Log.Debug("tunnel: candidate profile",
			"node", fmt.Sprintf("%x", p.NodeID[:4]),
			"successes", p.Successes,
			"failures", p.Failures,
			"success_ema", p.SuccessEMA,
			"latency_ema", p.LatencyEMA,
			"consecutive_failures", p.ConsecutiveFailures)
	}
}

func (m *TunnelManager) removeSession(s *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, s.Tunnel.ID)
	for destID, last := range m.lastSession {
		if last != nil && last.Tunnel.ID == s.Tunnel.ID {
			delete(m.lastSession, destID)
		}
	}
	for destID, pool := range m.pools {
		newPool := make([]*Session, 0, len(pool))
		for _, ps := range pool {
			if ps.Tunnel.ID != s.Tunnel.ID {
				newPool = append(newPool, ps)
			}
		}
		if len(newPool) == 0 {
			delete(m.pools, destID)
		} else {
			m.pools[destID] = newPool
		}
	}
}

func (m *TunnelManager) StartExpireLoop(ctx context.Context) {
	go m.expireLoop(ctx)
}

func (m *TunnelManager) expireLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.expireSessions()
		}
	}
}

func (m *TunnelManager) expireSessions() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.sessions {
		if s.Tunnel.Expired() {
			m.Log.Info("tunnel: expired",
				"tunnel_id", id.Short(),
				"state", s.Tunnel.State())
			s.Close()
			delete(m.sessions, id)
			for destID, last := range m.lastSession {
				if last != nil && last.Tunnel.ID == id {
					delete(m.lastSession, destID)
				}
			}
		}
	}
	for destID, pool := range m.pools {
		alive := make([]*Session, 0, len(pool))
		for _, s := range pool {
			if !s.Tunnel.State().IsTerminal() && !s.Tunnel.Expired() {
				alive = append(alive, s)
			}
		}
		if len(alive) == 0 {
			delete(m.pools, destID)
		} else {
			m.pools[destID] = alive
		}
	}
}
