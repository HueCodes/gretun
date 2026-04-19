//go:build linux

package daemon

import (
	"encoding/hex"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/HueCodes/gretun/internal/disco"
	"github.com/prometheus/client_golang/prometheus"
)

func TestPeerState_UnknownValue(t *testing.T) {
	s := peerState(42)
	got := s.String()
	if !strings.Contains(got, "42") {
		t.Errorf("unknown peerState should include raw value, got %q", got)
	}
}

func TestAbsorbEndpoints_Empty(t *testing.T) {
	p := &peerFSM{}
	p.absorbEndpoints(nil)
	if len(p.peer.Endpoints) != 0 {
		t.Errorf("empty input should leave endpoints empty, got %d", len(p.peer.Endpoints))
	}

	p2 := &peerFSM{}
	p2.absorbEndpoints([]string{})
	if len(p2.peer.Endpoints) != 0 {
		t.Error("empty slice should be a no-op")
	}
}

func TestAbsorbEndpoints_InvalidAddrs(t *testing.T) {
	p := &peerFSM{}
	p.absorbEndpoints([]string{"not-an-addr", "also:garbage:too:many:colons", ""})
	if len(p.peer.Endpoints) != 0 {
		t.Errorf("all invalid; want 0 endpoints, got %d", len(p.peer.Endpoints))
	}
}

func TestAbsorbEndpoints_MixedValidInvalid(t *testing.T) {
	p := &peerFSM{}
	p.absorbEndpoints([]string{"1.2.3.4:5", "bogus", "6.7.8.9:10"})
	if len(p.peer.Endpoints) != 2 {
		t.Errorf("want 2 valid, got %d: %+v", len(p.peer.Endpoints), p.peer.Endpoints)
	}
}

func TestAbsorbEndpoints_DedupAgainstExisting(t *testing.T) {
	ap, _ := netip.ParseAddrPort("1.2.3.4:5")
	p := &peerFSM{}
	p.peer.Endpoints = []disco.RemoteEndpoint{{Addr: ap, Source: "stun"}}
	p.absorbEndpoints([]string{"1.2.3.4:5", "9.9.9.9:99"})
	if len(p.peer.Endpoints) != 2 {
		t.Errorf("want 2 (existing + new), got %d", len(p.peer.Endpoints))
	}
}

func TestAbsorbEndpoints_IPv6(t *testing.T) {
	p := &peerFSM{}
	p.absorbEndpoints([]string{"[2001:db8::1]:5555"})
	if len(p.peer.Endpoints) != 1 {
		t.Errorf("want 1 v6 endpoint, got %d", len(p.peer.Endpoints))
	}
}

func TestNewPeerFSM_InitialState(t *testing.T) {
	fsm := newPeerFSM(peerDeps{}, disco.RemotePeer{Name: "alice"})
	if fsm.state != stateUnknown {
		t.Errorf("initial state should be unknown, got %v", fsm.state)
	}
	if fsm.incoming == nil || cap(fsm.incoming) != 32 {
		t.Errorf("incoming channel should have capacity 32")
	}
	if fsm.done == nil {
		t.Error("done channel should be initialised")
	}
	if fsm.peer.Name != "alice" {
		t.Errorf("peer not stored: %+v", fsm.peer)
	}
}

func TestStop_Idempotent(t *testing.T) {
	fsm := newPeerFSM(peerDeps{}, disco.RemotePeer{})
	fsm.stop()
	fsm.stop() // second call must not panic on already-closed channel
	select {
	case <-fsm.done:
	default:
		t.Error("done channel should be closed after stop")
	}
}

func TestUpdate_EnqueuesEvent(t *testing.T) {
	fsm := newPeerFSM(peerDeps{}, disco.RemotePeer{Name: "a"})
	newPeer := disco.RemotePeer{Name: "renamed"}
	fsm.update(newPeer)
	select {
	case ev := <-fsm.incoming:
		if ev.kind != evUpdate {
			t.Errorf("wrong event kind: %v", ev.kind)
		}
	default:
		t.Error("expected event in channel")
	}
	if fsm.peer.Name != "renamed" {
		t.Errorf("peer snapshot not updated: %+v", fsm.peer)
	}
}

func TestUpdate_DropsWhenChannelFull(t *testing.T) {
	fsm := newPeerFSM(peerDeps{}, disco.RemotePeer{})
	// Fill the channel; further sends must not block.
	for i := 0; i < cap(fsm.incoming)+5; i++ {
		fsm.update(disco.RemotePeer{})
	}
	// If we got here, the non-blocking send worked. Drain.
	for len(fsm.incoming) > 0 {
		<-fsm.incoming
	}
}

func TestOnUDP_EnqueuesWithBody(t *testing.T) {
	fsm := newPeerFSM(peerDeps{}, disco.RemotePeer{})
	ap, _ := netip.ParseAddrPort("1.2.3.4:5")
	fsm.onUDP(ap, disco.Body{Type: disco.MsgPing, Tx: "abc"})
	ev := <-fsm.incoming
	if ev.kind != evUDP || ev.addr != ap || ev.body.Tx != "abc" {
		t.Errorf("event wrong: %+v", ev)
	}
}

func TestOnSignal_MarksSignalTrue(t *testing.T) {
	fsm := newPeerFSM(peerDeps{}, disco.RemotePeer{})
	fsm.onSignal(disco.Body{Type: disco.MsgCallMeMaybe, Endpoints: []string{"1.2.3.4:5"}})
	ev := <-fsm.incoming
	if ev.kind != evSignal {
		t.Errorf("kind = %v, want evSignal", ev.kind)
	}
	if !ev.signal {
		t.Error("signal flag should be true")
	}
	if len(ev.body.Endpoints) != 1 {
		t.Errorf("body lost: %+v", ev.body)
	}
}

func TestSetState_Transition(t *testing.T) {
	fsm := newPeerFSM(peerDeps{}, disco.RemotePeer{Name: "alice"})
	fsm.setState(stateHaveEndpoints)
	if fsm.state != stateHaveEndpoints {
		t.Errorf("state not updated")
	}
	// Re-applying same state is a no-op for the log; state field unchanged.
	fsm.setState(stateHaveEndpoints)
	if fsm.state != stateHaveEndpoints {
		t.Error("state should still be stateHaveEndpoints")
	}
}

func TestOnPeerUpdate_TransitionsToPunching(t *testing.T) {
	fsm := newPeerFSM(peerDeps{}, disco.RemotePeer{})
	ap, _ := netip.ParseAddrPort("1.2.3.4:5")
	fsm.peer.Endpoints = []disco.RemoteEndpoint{{Addr: ap, Source: "stun"}}

	var deadline time.Time
	fsm.onPeerUpdate(&deadline)

	if fsm.state != statePunching {
		t.Errorf("state = %v, want statePunching", fsm.state)
	}
	if deadline.IsZero() {
		t.Error("deadline should have been set")
	}
}

func TestOnPeerUpdate_NoEndpointsIsNoop(t *testing.T) {
	fsm := newPeerFSM(peerDeps{}, disco.RemotePeer{})
	var deadline time.Time
	fsm.onPeerUpdate(&deadline)
	if fsm.state != stateUnknown {
		t.Errorf("with no endpoints, state should stay unknown; got %v", fsm.state)
	}
}

func TestNewTxID_FormatAndRandomness(t *testing.T) {
	a := newTxID()
	b := newTxID()
	if len(a) != 32 {
		t.Errorf("txID = %q len=%d, want 32 hex chars", a, len(a))
	}
	if _, err := hex.DecodeString(a); err != nil {
		t.Errorf("txID not valid hex: %v", err)
	}
	if a == b {
		t.Errorf("consecutive txIDs should differ: %q == %q", a, b)
	}
}

func TestMustAddrFromIP_V4(t *testing.T) {
	got := mustAddrFromIP(net.IPv4(10, 0, 0, 1))
	if !got.IsValid() {
		t.Error("should return valid addr for v4")
	}
	if got.String() != "10.0.0.1" {
		t.Errorf("got %v", got)
	}
}

func TestMustAddrFromIP_V6(t *testing.T) {
	v6 := net.ParseIP("2001:db8::1")
	got := mustAddrFromIP(v6)
	if !got.IsValid() {
		t.Error("should return valid addr for v6")
	}
}

func TestNewMetrics_RegistersAllCollectors(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	if m.PeersByState == nil || m.DiscoPingsSent == nil ||
		m.DiscoPongsRecv == nil || m.HolePunchDuration == nil {
		t.Fatal("all collectors should be non-nil")
	}

	// Bump counters and confirm the registered collector reflects the changes.
	m.DiscoPingsSent.Inc()
	m.DiscoPongsRecv.Add(3)
	m.HolePunchDuration.Observe(0.5)
	m.PeersByState.WithLabelValues("direct").Set(2)

	got, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	names := make(map[string]bool)
	for _, mf := range got {
		names[*mf.Name] = true
	}
	for _, want := range []string{
		"gretun_peers",
		"gretun_disco_pings_sent_total",
		"gretun_disco_pongs_received_total",
		"gretun_hole_punch_duration_seconds",
	} {
		if !names[want] {
			t.Errorf("metric %q not registered", want)
		}
	}
}

func TestNewMetrics_NilRegistryIsAllowed(t *testing.T) {
	m := NewMetrics(nil)
	if m == nil {
		t.Fatal("NewMetrics(nil) should still return a Metrics")
	}
	// The collectors should be usable even if not registered.
	m.DiscoPingsSent.Inc()
}

func TestGatherLocalAddrPorts_ExtractsPort(t *testing.T) {
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Skip("cannot bind UDP: ", err)
	}
	defer conn.Close()

	eps := gatherLocalAddrPorts(conn)
	port := conn.LocalAddr().(*net.UDPAddr).Port
	wantSuffix := ":" + itoa(port)
	for _, e := range eps {
		if !strings.HasSuffix(e, wantSuffix) {
			t.Errorf("endpoint %q missing expected port suffix %q", e, wantSuffix)
		}
	}
}

// itoa avoids pulling strconv into this file twice.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var digits []byte
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}
