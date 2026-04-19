package coord

import (
	"context"
	"crypto/ed25519"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *MemStore {
	t.Helper()
	return NewMemStore(netip.MustParsePrefix("100.64.0.0/24"))
}

func makePeer(t *testing.T, name string) Peer {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	var dk [32]byte
	copy(dk[:], []byte(name+"-disco-key-padding-padding-padding")[:32])
	return Peer{NodeKey: pub, DiscoKey: dk, Name: name}
}

func TestStore_Register_BadKeyLength(t *testing.T) {
	s := newTestStore(t)
	p := Peer{NodeKey: []byte{1, 2, 3}}
	if _, err := s.Register(context.Background(), p); err == nil {
		t.Error("expected bad key length error")
	}
}

func TestStore_Register_Idempotent(t *testing.T) {
	s := newTestStore(t)
	p := makePeer(t, "alice")
	ip1, err := s.Register(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}

	// Register again with same node key → same tunnel IP, updated name/disco.
	p.Name = "alice-renamed"
	ip2, err := s.Register(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if ip1 != ip2 {
		t.Errorf("reregister should preserve tunnel IP: %v vs %v", ip1, ip2)
	}

	peers, _, _ := s.Peers(context.Background())
	if len(peers) != 1 || peers[0].Name != "alice-renamed" {
		t.Errorf("reregister should update existing peer, got %+v", peers)
	}
}

func TestStore_Register_SequentialIPs(t *testing.T) {
	s := NewMemStore(netip.MustParsePrefix("100.64.0.0/24"))
	ips := make(map[netip.Addr]bool)
	for i := 0; i < 5; i++ {
		ip, err := s.Register(context.Background(), makePeer(t, "n"))
		if err != nil {
			t.Fatal(err)
		}
		if ips[ip] {
			t.Errorf("duplicate IP allocated: %v", ip)
		}
		ips[ip] = true
	}
	if len(ips) != 5 {
		t.Errorf("want 5 distinct IPs, got %d", len(ips))
	}
}

func TestStore_AllocateIP_Exhaustion(t *testing.T) {
	// /30 has 4 addresses: .0 (network), .1, .2, .3 (broadcast).
	// allocateIPLocked skips network (.0) and broadcast (.3), leaving .1 and .2.
	s := NewMemStore(netip.MustParsePrefix("10.0.0.0/30"))
	if _, err := s.Register(context.Background(), makePeer(t, "a")); err != nil {
		t.Fatalf("alloc 1: %v", err)
	}
	if _, err := s.Register(context.Background(), makePeer(t, "b")); err != nil {
		t.Fatalf("alloc 2: %v", err)
	}
	if _, err := s.Register(context.Background(), makePeer(t, "c")); err == nil {
		t.Errorf("expected exhaustion error on third allocation")
	}
}

func TestStore_Register_ContextCancelled(t *testing.T) {
	s := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Register(ctx, makePeer(t, "a")); err == nil {
		t.Error("expected context error")
	}
}

func TestStore_SetEndpoints_UnknownPeer(t *testing.T) {
	s := newTestStore(t)
	// Peer never registered.
	unknown, _, _ := ed25519.GenerateKey(nil)
	err := s.SetEndpoints(context.Background(), unknown, []Endpoint{
		{Addr: netip.MustParseAddrPort("1.2.3.4:5"), Source: SourceSTUN},
	})
	if err == nil {
		t.Error("expected error for unknown peer")
	}
}

func TestStore_SetEndpoints_ReplacesNotAppends(t *testing.T) {
	s := newTestStore(t)
	p := makePeer(t, "alice")
	if _, err := s.Register(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	first := []Endpoint{{Addr: netip.MustParseAddrPort("1.1.1.1:1"), Source: SourceSTUN}}
	if err := s.SetEndpoints(context.Background(), p.NodeKey, first); err != nil {
		t.Fatal(err)
	}
	second := []Endpoint{
		{Addr: netip.MustParseAddrPort("2.2.2.2:2"), Source: SourceSTUN},
		{Addr: netip.MustParseAddrPort("3.3.3.3:3"), Source: SourceLocal},
	}
	if err := s.SetEndpoints(context.Background(), p.NodeKey, second); err != nil {
		t.Fatal(err)
	}
	peers, _, _ := s.Peers(context.Background())
	if len(peers[0].Endpoints) != 2 {
		t.Errorf("SetEndpoints should replace, got %d endpoints", len(peers[0].Endpoints))
	}
}

func TestStore_Peers_ContextCancelled(t *testing.T) {
	s := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := s.Peers(ctx); err == nil {
		t.Error("expected context error")
	}
}

func TestStore_Peers_SortedByTunnelIP(t *testing.T) {
	s := newTestStore(t)
	for i := 0; i < 4; i++ {
		if _, err := s.Register(context.Background(), makePeer(t, "n")); err != nil {
			t.Fatal(err)
		}
	}
	peers, _, _ := s.Peers(context.Background())
	for i := 1; i < len(peers); i++ {
		if !peers[i-1].TunnelIP.Less(peers[i].TunnelIP) {
			t.Errorf("peers not sorted by tunnel IP: %v then %v", peers[i-1].TunnelIP, peers[i].TunnelIP)
		}
	}
}

func TestStore_WaitForPeersChange_ImmediateReturnOnStaleEtag(t *testing.T) {
	s := newTestStore(t)
	// "since" doesn't match current etag → return immediately.
	start := time.Now()
	if err := s.WaitForPeersChange(context.Background(), "different-etag"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Error("should have returned immediately")
	}
}

func TestStore_WaitForPeersChange_CancelledContext(t *testing.T) {
	s := newTestStore(t)
	_, etag, _ := s.Peers(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := s.WaitForPeersChange(ctx, etag); err == nil {
		t.Error("expected context error after timeout")
	}
}

func TestStore_WaitForPeersChange_WakesOnRegister(t *testing.T) {
	s := newTestStore(t)
	_, etag, _ := s.Peers(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- s.WaitForPeersChange(context.Background(), etag)
	}()

	time.Sleep(30 * time.Millisecond)
	if _, err := s.Register(context.Background(), makePeer(t, "x")); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("waiter returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("waiter did not wake on register")
	}
}

func TestStore_EnqueuePopSignals(t *testing.T) {
	s := newTestStore(t)
	var to [32]byte
	to[0] = 1
	env := Envelope{Sealed: []byte("hi"), Enqueue: time.Now()}
	if err := s.EnqueueSignal(context.Background(), to, env); err != nil {
		t.Fatal(err)
	}
	got, err := s.PopSignals(context.Background(), to)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 envelope, got %d", len(got))
	}
	// Pop should drain.
	again, _ := s.PopSignals(context.Background(), to)
	if len(again) != 0 {
		t.Errorf("second pop should be empty, got %d", len(again))
	}
}

func TestStore_PopSignals_DropsStaleEnvelopes(t *testing.T) {
	s := newTestStore(t)
	s.maxAge = 10 * time.Millisecond
	var to [32]byte
	old := Envelope{Sealed: []byte("old"), Enqueue: time.Now().Add(-time.Second)}
	fresh := Envelope{Sealed: []byte("new"), Enqueue: time.Now()}
	_ = s.EnqueueSignal(context.Background(), to, old)
	_ = s.EnqueueSignal(context.Background(), to, fresh)

	got, _ := s.PopSignals(context.Background(), to)
	if len(got) != 1 || string(got[0].Sealed) != "new" {
		t.Errorf("want only fresh envelope, got %+v", got)
	}
}

func TestStore_EnqueueSignal_RingBuffer(t *testing.T) {
	s := newTestStore(t)
	s.maxQueueDepth = 3
	var to [32]byte

	for i := 0; i < 5; i++ {
		e := Envelope{Sealed: []byte{byte(i)}, Enqueue: time.Now()}
		if err := s.EnqueueSignal(context.Background(), to, e); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := s.PopSignals(context.Background(), to)
	if len(got) != 3 {
		t.Fatalf("want 3 retained, got %d", len(got))
	}
	// Oldest two should be dropped → first retained envelope has Sealed=[2].
	if got[0].Sealed[0] != 2 {
		t.Errorf("ring buffer kept the wrong element: %v", got[0].Sealed)
	}
}

func TestStore_WaitForSignal_ReturnsImmediatelyIfQueued(t *testing.T) {
	s := newTestStore(t)
	var to [32]byte
	_ = s.EnqueueSignal(context.Background(), to, Envelope{Sealed: []byte("x"), Enqueue: time.Now()})
	start := time.Now()
	if err := s.WaitForSignal(context.Background(), to); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Error("should have returned immediately when queue non-empty")
	}
}

func TestStore_WaitForSignal_WakesOnEnqueue(t *testing.T) {
	s := newTestStore(t)
	var to [32]byte

	done := make(chan error, 1)
	go func() {
		done <- s.WaitForSignal(context.Background(), to)
	}()

	time.Sleep(30 * time.Millisecond)
	_ = s.EnqueueSignal(context.Background(), to, Envelope{Sealed: []byte("x"), Enqueue: time.Now()})

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("waiter error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("waiter did not wake")
	}
}

func TestStore_WaitForSignal_CancelledContext(t *testing.T) {
	s := newTestStore(t)
	var to [32]byte
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := s.WaitForSignal(ctx, to); err == nil {
		t.Error("expected context error")
	}
}

func TestNewEtag_Varies(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 5; i++ {
		e := newEtag()
		if e == "" {
			t.Error("etag should not be empty")
		}
		seen[e] = true
		time.Sleep(2 * time.Millisecond)
	}
	if len(seen) < 2 {
		t.Errorf("etag should vary across time, got %d unique values", len(seen))
	}
}

func TestBase64Encode_Deterministic(t *testing.T) {
	a := base64Encode([]byte{1, 2, 3})
	b := base64Encode([]byte{1, 2, 3})
	if a != b {
		t.Error("base64Encode is not deterministic")
	}
	if !strings.EqualFold(a, "010203") {
		t.Errorf("expected hex encoding %q, got %q", "010203", a)
	}
}
