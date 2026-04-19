//go:build linux

package daemon

import (
	"net/netip"
	"testing"
)

func TestPeerState_String(t *testing.T) {
	cases := []struct {
		s peerState
		w string
	}{
		{stateUnknown, "unknown"},
		{stateHaveEndpoints, "have_endpoints"},
		{statePunching, "punching"},
		{stateDirect, "direct"},
		{stateRelay, "relay"},
	}
	for _, c := range cases {
		if got := c.s.String(); got != c.w {
			t.Errorf("state(%d).String() = %q, want %q", int(c.s), got, c.w)
		}
	}
}

func TestFirstGlobalV4_ReturnsSomething(t *testing.T) {
	// Best-effort: CI runners always have at least one non-loopback v4.
	addr := firstGlobalV4()
	if !addr.IsValid() {
		t.Skip("no global v4 on this host; skipping")
	}
	if addr.IsLoopback() {
		t.Errorf("expected non-loopback; got %v", addr)
	}
}

func TestAbsorbEndpoints_Dedup(t *testing.T) {
	p := &peerFSM{}
	p.absorbEndpoints([]string{"1.2.3.4:5555", "1.2.3.4:5555", "6.7.8.9:10"})
	if len(p.peer.Endpoints) != 2 {
		t.Fatalf("want 2 unique endpoints, got %d: %+v", len(p.peer.Endpoints), p.peer.Endpoints)
	}
	seen := make(map[netip.AddrPort]bool)
	for _, e := range p.peer.Endpoints {
		if seen[e.Addr] {
			t.Errorf("duplicate endpoint: %v", e.Addr)
		}
		seen[e.Addr] = true
	}
}
