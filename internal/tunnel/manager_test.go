package tunnel

import (
	"testing"
	"time"
)

func TestNewID_Unique(t *testing.T) {
	seen := make(map[ID]bool)
	for i := 0; i < 100; i++ {
		id := MustNewID()
		if seen[id] {
			t.Fatalf("duplicate id: %s", id)
		}
		seen[id] = true
	}
}

func TestState_Transitions(t *testing.T) {
	id := MustNewID()
	init := Hop{Addr: "init:9001", Type: HopInitiator}
	dest := Hop{Addr: "dest:9002", Type: HopDest}
	path := []Hop{init, {Addr: "r1:9100", Type: HopRelay}, dest}

	tun := NewTunnel(id, init, dest, path, DefaultConfig())

	if tun.State() != StateBuilding {
		t.Fatalf("initial state: %v", tun.State())
	}
	if !tun.State().CanSend() && tun.State() == StateBuilding {
	}

	tun.Activate(&E2ESession{SendKey: make([]byte, 32), RecvKey: make([]byte, 32)})
	if tun.State() != StateActive {
		t.Fatalf("after activate: %v", tun.State())
	}
	if !tun.State().CanSend() {
		t.Fatalf("ACTIVE should be able to send")
	}

	tun.SetState(StateDegraded)
	if !tun.State().CanSend() {
		t.Fatalf("DEGRADED should be able to send")
	}

	tun.SetState(StateDead)
	if !tun.State().IsTerminal() {
		t.Fatalf("DEAD should be terminal")
	}

	tun.SetState(StateActive)
	if tun.State() != StateDead {
		t.Fatalf("DEAD should be sticky, got %v", tun.State())
	}
}

func TestNumRelays(t *testing.T) {
	init := Hop{Type: HopInitiator}
	dest := Hop{Type: HopDest}
	cases := []struct {
		path   []Hop
		expect int
	}{
		{[]Hop{init, dest}, 0},
		{[]Hop{init, {Type: HopRelay}, dest}, 1},
		{[]Hop{init, {Type: HopRelay}, {Type: HopRelay}, {Type: HopRelay}, dest}, 3},
	}
	for _, c := range cases {
		tun := NewTunnel(MustNewID(), init, dest, c.path, DefaultConfig())
		if tun.NumRelays() != c.expect {
			t.Fatalf("path len %d: got %d, want %d", len(c.path), tun.NumRelays(), c.expect)
		}
	}
}

func TestManager_AddGetRemove(t *testing.T) {
	m := NewManager()
	id := MustNewID()
	tun := NewTunnel(id, Hop{}, Hop{}, nil, DefaultConfig())

	m.Add(tun)
	if m.Count() != 1 {
		t.Fatalf("count: %d", m.Count())
	}

	got, ok := m.Get(id)
	if !ok || got.ID != id {
		t.Fatal("Get failed")
	}

	m.Remove(id)
	if m.Count() != 0 {
		t.Fatalf("count after remove: %d", m.Count())
	}
}

func TestManager_Active(t *testing.T) {
	m := NewManager()
	destID := [32]byte{0xAA}

	active := NewTunnel(MustNewID(), Hop{}, Hop{NodeID: destID}, nil, DefaultConfig())
	active.Activate(&E2ESession{})
	m.Add(active)

	building := NewTunnel(MustNewID(), Hop{}, Hop{NodeID: destID}, nil, DefaultConfig())
	m.Add(building)

	dead := NewTunnel(MustNewID(), Hop{}, Hop{NodeID: destID}, nil, DefaultConfig())
	dead.SetState(StateDead)
	m.Add(dead)

	other := NewTunnel(MustNewID(), Hop{}, Hop{NodeID: [32]byte{0xBB}}, nil, DefaultConfig())
	other.Activate(&E2ESession{})
	m.Add(other)

	list := m.Active(destID)
	if len(list) != 1 {
		t.Fatalf("active count: %d, want 1", len(list))
	}
	if list[0].ID != active.ID {
		t.Fatal("wrong tunnel")
	}
}

func TestManager_CleanupDead(t *testing.T) {
	m := NewManager()
	for i := 0; i < 3; i++ {
		tun := NewTunnel(MustNewID(), Hop{}, Hop{}, nil, DefaultConfig())
		tun.SetState(StateDead)
		m.Add(tun)
	}
	active := NewTunnel(MustNewID(), Hop{}, Hop{}, nil, DefaultConfig())
	active.Activate(&E2ESession{})
	m.Add(active)

	removed := m.CleanupDead()
	if removed != 3 {
		t.Fatalf("removed: %d, want 3", removed)
	}
	if m.Count() != 1 {
		t.Fatalf("count after cleanup: %d", m.Count())
	}
}

func TestE2ESession_Nonce(t *testing.T) {
	e := &E2ESession{}

	if e.NextSendNonce() != 0 {
		t.Fatal("first send nonce should be 0")
	}
	if e.NextSendNonce() != 1 {
		t.Fatal("second send nonce should be 1")
	}

	if err := e.CheckRecvNonce(0); err != nil {
		t.Fatalf("recv 0: %v", err)
	}
	if err := e.CheckRecvNonce(1); err != nil {
		t.Fatalf("recv 1: %v", err)
	}
	if err := e.CheckRecvNonce(0); err != ErrReplay {
		t.Fatalf("recv 0 replay: %v", err)
	}
	if err := e.CheckRecvNonce(5); err != ErrNonceGap {
		t.Fatalf("recv 5 gap: %v", err)
	}
}

func TestTunnel_Stats(t *testing.T) {
	init := Hop{NodeID: [32]byte{1}}
	dest := Hop{NodeID: [32]byte{2}}
	path := []Hop{init, {NodeID: [32]byte{3}, Type: HopRelay}, dest}
	tun := NewTunnel(MustNewID(), init, dest, path, DefaultConfig())
	tun.Activate(&E2ESession{})
	tun.IncSent()
	tun.IncSent()
	tun.IncRecv()

	time.Sleep(1 * time.Millisecond)

	st := tun.Stats()
	if st.NumRelays != 1 {
		t.Fatalf("num relays: %d", st.NumRelays)
	}
	if st.MessagesSent != 2 {
		t.Fatalf("sent: %d", st.MessagesSent)
	}
	if st.MessagesRecv != 1 {
		t.Fatalf("recv: %d", st.MessagesRecv)
	}
	if st.State != StateActive {
		t.Fatalf("state: %v", st.State)
	}
}
