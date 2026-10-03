package tunnel

import (
	"errors"
	"testing"
	"time"
)

type mockSource struct {
	destAddr string
	destErr  error

	peers   [][32]byte
	addrs   map[[32]byte]string
	pingErr map[string]error
	pingRTT time.Duration
}

func newMockSource() *mockSource {
	return &mockSource{
		addrs:   make(map[[32]byte]string),
		pingErr: make(map[string]error),
		pingRTT: 5 * time.Millisecond,
	}
}

func (m *mockSource) FindDest(destID [32]byte) (string, []byte, error) {
	if m.destErr != nil {
		return "", nil, m.destErr
	}
	return m.destAddr, []byte("fake-pubkey"), nil
}

func (m *mockSource) KnownPeers() [][32]byte {
	return m.peers
}

func (m *mockSource) AddrOf(nodeID [32]byte) (string, bool) {
	a, ok := m.addrs[nodeID]
	return a, ok
}

func (m *mockSource) Ping(addr string) (time.Duration, error) {
	if err, ok := m.pingErr[addr]; ok {
		return 0, err
	}
	return m.pingRTT, nil
}

func TestRouteBuilder_BuildOK(t *testing.T) {
	src := newMockSource()
	src.destAddr = "dest:9002"
	destID := [32]byte{0xFF}

	r1 := [32]byte{0x01}
	r2 := [32]byte{0x02}
	r3 := [32]byte{0x03}
	r4 := [32]byte{0x04}
	src.peers = [][32]byte{r1, r2, r3, r4}
	src.addrs[r1] = "r1:9100"
	src.addrs[r2] = "r2:9101"
	src.addrs[r3] = "r3:9102"
	src.addrs[r4] = "r4:9103"

	profiles := NewProfileStore()
	localID := [32]byte{0xAA}
	rb := NewRouteBuilder(localID, "self:9001", 3, src, profiles)

	path, err := rb.Build(destID, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(path) != 5 {
		t.Fatalf("path len: %d", len(path))
	}
	if path[0].Type != HopInitiator {
		t.Fatalf("first hop should be initiator")
	}
	if path[len(path)-1].Type != HopDest {
		t.Fatalf("last hop should be dest")
	}
	seen := map[[32]byte]bool{}
	for _, h := range path[1 : len(path)-1] {
		if seen[h.NodeID] {
			t.Fatal("duplicate relay")
		}
		seen[h.NodeID] = true
	}
}

func TestRouteBuilder_NotEnoughPeers(t *testing.T) {
	src := newMockSource()
	src.destAddr = "dest:9002"
	src.peers = [][32]byte{{0x01}}

	profiles := NewProfileStore()
	rb := NewRouteBuilder([32]byte{0xAA}, "self:9001", 3, src, profiles)

	_, err := rb.Build([32]byte{0xFF}, nil)
	if !errors.Is(err, ErrNoCandidates) {
		t.Fatalf("want ErrNoCandidates, got %v", err)
	}
}

func TestRouteBuilder_DeadRelay(t *testing.T) {
	src := newMockSource()
	src.destAddr = "dest:9002"
	destID := [32]byte{0xFF}

	r1 := [32]byte{0x01}
	r2 := [32]byte{0x02}
	r3 := [32]byte{0x03}
	r4 := [32]byte{0x04}
	r5 := [32]byte{0x05}
	src.peers = [][32]byte{r1, r2, r3, r4, r5}
	src.addrs[r1] = "r1:9100"
	src.addrs[r2] = "r2:9101"
	src.addrs[r3] = "r3:9102"
	src.addrs[r4] = "r4:9103"
	src.addrs[r5] = "r5:9104"
	src.pingErr["r1:9100"] = errors.New("timeout")

	profiles := NewProfileStore()
	rb := NewRouteBuilder([32]byte{0xAA}, "self:9001", 4, src, profiles)

	path, err := rb.Build(destID, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, h := range path[1 : len(path)-1] {
		if h.NodeID == r1 {
			t.Fatal("dead relay should not be in path")
		}
	}
	p, ok := profiles.Get(r1)
	if !ok {
		t.Fatal("profile not created for dead relay")
	}
	if p.Failures != 1 {
		t.Fatalf("failures: %d", p.Failures)
	}
}

func TestRouteBuilder_DestUnreachable(t *testing.T) {
	src := newMockSource()
	src.destErr = errors.New("not found")

	profiles := NewProfileStore()
	rb := NewRouteBuilder([32]byte{0xAA}, "self:9001", 3, src, profiles)

	_, err := rb.Build([32]byte{0xFF}, nil)
	if err == nil {
		t.Fatal("want error")
	}
}

func TestRouteBuilder_Exclude(t *testing.T) {
	src := newMockSource()
	src.destAddr = "dest:9002"
	destID := [32]byte{0xFF}

	r1 := [32]byte{0x01}
	r2 := [32]byte{0x02}
	r3 := [32]byte{0x03}
	r4 := [32]byte{0x04}
	src.peers = [][32]byte{r1, r2, r3, r4}
	src.addrs[r1] = "r1:9100"
	src.addrs[r2] = "r2:9101"
	src.addrs[r3] = "r3:9102"
	src.addrs[r4] = "r4:9103"

	profiles := NewProfileStore()
	rb := NewRouteBuilder([32]byte{0xAA}, "self:9001", 2, src, profiles)

	path1, err := rb.Build(destID, nil)
	if err != nil {
		t.Fatalf("build1: %v", err)
	}
	used := map[[32]byte]bool{}
	for _, h := range path1[1 : len(path1)-1] {
		used[h.NodeID] = true
	}

	var exclude [][32]byte
	for id := range used {
		exclude = append(exclude, id)
	}
	path2, err := rb.Build(destID, exclude)
	if err != nil {
		t.Fatalf("build2: %v", err)
	}
	for _, h := range path2[1 : len(path2)-1] {
		if used[h.NodeID] {
			t.Fatalf("relay %x reused despite exclude", h.NodeID[:2])
		}
	}
}

func TestRouteBuilder_ExcludeNotEnough(t *testing.T) {
	src := newMockSource()
	src.destAddr = "dest:9002"
	destID := [32]byte{0xFF}

	r1 := [32]byte{0x01}
	r2 := [32]byte{0x02}
	src.peers = [][32]byte{r1, r2}
	src.addrs[r1] = "r1:9100"
	src.addrs[r2] = "r2:9101"

	profiles := NewProfileStore()
	rb := NewRouteBuilder([32]byte{0xAA}, "self:9001", 2, src, profiles)

	_, err := rb.Build(destID, [][32]byte{r1, r2})
	if !errors.Is(err, ErrNoCandidates) {
		t.Fatalf("want ErrNoCandidates, got %v", err)
	}
}
