//go:build linux

package tunnel

import (
	"context"
	"fmt"
	"net"
	"testing"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func TestFouEncapConst(t *testing.T) {
	if fouEncapConst(EncapGUE) != netlink.FOU_ENCAP_GUE {
		t.Error("GUE → FOU_ENCAP_GUE")
	}
	if fouEncapConst(EncapFOU) != netlink.FOU_ENCAP_DIRECT {
		t.Error("FOU → FOU_ENCAP_DIRECT")
	}
	if fouEncapConst(EncapNone) != netlink.FOU_ENCAP_DIRECT {
		t.Error("None → FOU_ENCAP_DIRECT (default)")
	}
}

func TestNetlinkEncapConst(t *testing.T) {
	if netlinkEncapConst(EncapGUE) != netlink.FOU_ENCAP_GUE {
		t.Error("GUE → FOU_ENCAP_GUE")
	}
	if netlinkEncapConst(EncapFOU) != netlink.FOU_ENCAP_DIRECT {
		t.Error("FOU → FOU_ENCAP_DIRECT")
	}
}

func TestApplyEncap_None(t *testing.T) {
	gre := &netlink.Gretun{}
	applyEncap(gre, Config{Encap: EncapNone})
	if gre.EncapType != 0 || gre.EncapSport != 0 || gre.EncapDport != 0 || gre.EncapFlags != 0 {
		t.Errorf("EncapNone should leave gre encap fields zeroed: %+v", gre)
	}
}

func TestApplyEncap_FOU_WithChecksum(t *testing.T) {
	gre := &netlink.Gretun{}
	applyEncap(gre, Config{
		Encap:         EncapFOU,
		EncapSport:    1234,
		EncapDport:    5678,
		EncapChecksum: true,
	})
	if gre.EncapType != uint16(netlink.FOU_ENCAP_DIRECT) {
		t.Errorf("EncapType = %d, want FOU_ENCAP_DIRECT", gre.EncapType)
	}
	if gre.EncapSport != 1234 || gre.EncapDport != 5678 {
		t.Errorf("ports not set: sport=%d dport=%d", gre.EncapSport, gre.EncapDport)
	}
	if gre.EncapFlags != tunnelEncapFlagCSum {
		t.Errorf("EncapFlags missing CSum: %d", gre.EncapFlags)
	}
}

func TestApplyEncap_GUE_NoChecksum(t *testing.T) {
	gre := &netlink.Gretun{}
	applyEncap(gre, Config{Encap: EncapGUE, EncapDport: 7777})
	if gre.EncapType != uint16(netlink.FOU_ENCAP_GUE) {
		t.Errorf("EncapType wrong: %d", gre.EncapType)
	}
	if gre.EncapFlags != 0 {
		t.Errorf("EncapFlags should be 0 without checksum, got %d", gre.EncapFlags)
	}
}

func TestStatusFromGretun_TranslatesEncap(t *testing.T) {
	gre := &netlink.Gretun{
		LinkAttrs: netlink.LinkAttrs{Name: "tun0", MTU: 1400},
		Local:     net.IPv4(10, 0, 0, 1),
		Remote:    net.IPv4(10, 0, 0, 2),
		IKey:      42,
		Ttl:       64,
	}
	gre.EncapType = uint16(netlink.FOU_ENCAP_GUE)
	gre.EncapSport = 1111
	gre.EncapDport = 2222

	s := statusFromGretun(gre, gre)
	if s.Encap != "gue" {
		t.Errorf("encap = %q, want gue", s.Encap)
	}
	if s.EncapSport != 1111 || s.EncapDport != 2222 {
		t.Errorf("encap ports lost: %d/%d", s.EncapSport, s.EncapDport)
	}
	if s.MTU != 1400 {
		t.Errorf("MTU = %d, want 1400", s.MTU)
	}
	if s.Name != "tun0" || s.Key != 42 || s.TTL != 64 {
		t.Errorf("core fields wrong: %+v", s)
	}
}

func TestStatusFromGretun_FOU(t *testing.T) {
	gre := &netlink.Gretun{
		LinkAttrs: netlink.LinkAttrs{Name: "tun1"},
		Local:     net.IPv4(1, 2, 3, 4),
	}
	gre.EncapType = uint16(netlink.FOU_ENCAP_DIRECT)
	s := statusFromGretun(gre, gre)
	if s.Encap != "fou" {
		t.Errorf("want fou, got %q", s.Encap)
	}
}

func TestStatusFromGretun_NoEncap(t *testing.T) {
	gre := &netlink.Gretun{LinkAttrs: netlink.LinkAttrs{Name: "tun1"}}
	// EncapType 0 doesn't match either constant → empty encap string.
	s := statusFromGretun(gre, gre)
	if s.Encap != "" {
		t.Errorf("want empty encap, got %q", s.Encap)
	}
}

func TestStatusFromGretun_UpFlag(t *testing.T) {
	upGre := &netlink.Gretun{
		LinkAttrs: netlink.LinkAttrs{Name: "up", Flags: net.FlagUp},
	}
	if s := statusFromGretun(upGre, upGre); !s.Up {
		t.Error("expected Up=true")
	}

	downGre := &netlink.Gretun{LinkAttrs: netlink.LinkAttrs{Name: "down"}}
	if s := statusFromGretun(downGre, downGre); s.Up {
		t.Error("expected Up=false")
	}
}

func TestCreate_ContextCancelled(t *testing.T) {
	m := newMockNetlinker()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Create(ctx, m, Config{Name: "tun0", LocalIP: net.IPv4(1, 2, 3, 4), RemoteIP: net.IPv4(5, 6, 7, 8)})
	if err == nil {
		t.Error("expected context error")
	}
}

func TestDelete_ContextCancelled(t *testing.T) {
	m := newMockNetlinker()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Delete(ctx, m, "tun0"); err == nil {
		t.Error("expected context error")
	}
}

func TestAssignIP_ContextCancelled(t *testing.T) {
	m := newMockNetlinker()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := AssignIP(ctx, m, "tun0", "10.0.0.1/30"); err == nil {
		t.Error("expected context error")
	}
}

func TestGet_ContextCancelled(t *testing.T) {
	m := newMockNetlinker()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Get(ctx, m, "tun0"); err == nil {
		t.Error("expected context error")
	}
}

func TestList_ContextCancelled(t *testing.T) {
	m := newMockNetlinker()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := List(ctx, m); err == nil {
		t.Error("expected context error")
	}
}

func TestCreate_FOU_FouAddFails(t *testing.T) {
	m := newMockNetlinker()
	m.fouAddErr = fmt.Errorf("fou boom")
	cfg := Config{
		Name: "tun0", LocalIP: net.IPv4(1, 2, 3, 4), RemoteIP: net.IPv4(5, 6, 7, 8),
		Encap: EncapFOU, EncapDport: 5555,
	}
	if err := Create(context.Background(), m, cfg); err == nil {
		t.Fatal("expected FOU setup error")
	}
	if m.linkAddCalled {
		t.Error("LinkAdd should not be called after FouAdd failure")
	}
}

func TestCreate_FOU_EEXISTIsTolerated(t *testing.T) {
	m := newMockNetlinker()
	// Simulate port already open: FouAdd returns EEXIST.
	m.fouAddErr = unix.EEXIST
	cfg := Config{
		Name: "tun0", LocalIP: net.IPv4(10, 0, 0, 1), RemoteIP: net.IPv4(10, 0, 0, 2),
		Encap: EncapFOU, EncapDport: 5555,
	}
	if err := Create(context.Background(), m, cfg); err != nil {
		t.Fatalf("EEXIST should not abort Create: %v", err)
	}
	if !m.linkAddCalled {
		t.Error("LinkAdd should still run when FOU port already exists")
	}
}

func TestCreate_MTUFailureRollsBackLinkAndFOU(t *testing.T) {
	m := newMockNetlinker()
	m.linkSetMTUErr = fmt.Errorf("mtu boom")
	cfg := Config{
		Name: "tun0", LocalIP: net.IPv4(10, 0, 0, 1), RemoteIP: net.IPv4(10, 0, 0, 2),
		Encap: EncapFOU, EncapDport: 5555, MTU: 1400,
	}
	if err := Create(context.Background(), m, cfg); err == nil {
		t.Fatal("expected MTU error")
	}
	if !m.linkDelCalled {
		t.Error("LinkDel should run for cleanup")
	}
	if m.fouDelCalls == 0 {
		t.Error("FOU rollback should run when we created the FOU in this call")
	}
}

func TestCreate_SetUpFailureRollsBackFOU(t *testing.T) {
	m := newMockNetlinker()
	m.linkSetUpErr = fmt.Errorf("set up boom")
	cfg := Config{
		Name: "tun0", LocalIP: net.IPv4(10, 0, 0, 1), RemoteIP: net.IPv4(10, 0, 0, 2),
		Encap: EncapFOU, EncapDport: 5555,
	}
	if err := Create(context.Background(), m, cfg); err == nil {
		t.Fatal("expected set-up error")
	}
	if m.fouDelCalls == 0 {
		t.Error("expected FOU rollback after set-up failure")
	}
}

func TestRemoveFOU(t *testing.T) {
	m := newMockNetlinker()
	// Happy path: FouDel succeeds.
	RemoveFOU(m, 5555)
	if m.fouDelCalls != 1 {
		t.Errorf("want 1 FouDel call, got %d", m.fouDelCalls)
	}

	// Error path: FouDel fails. RemoveFOU logs but does not panic or return error.
	m2 := newMockNetlinker()
	m2.fouDelErr = fmt.Errorf("nope")
	RemoveFOU(m2, 5555)
	if m2.fouDelCalls != 1 {
		t.Errorf("want 1 FouDel call even on error, got %d", m2.fouDelCalls)
	}
}

func TestEnsureFOU_ReturnsTrueOnFreshAdd(t *testing.T) {
	m := newMockNetlinker()
	created, err := EnsureFOU(m, 5555, EncapFOU)
	if err != nil || !created {
		t.Errorf("want created=true err=nil, got created=%v err=%v", created, err)
	}
}

func TestEnsureFOU_ReturnsFalseOnEEXIST(t *testing.T) {
	m := newMockNetlinker()
	m.fouAddErr = unix.EEXIST
	created, err := EnsureFOU(m, 5555, EncapFOU)
	if err != nil {
		t.Errorf("EEXIST should not propagate: %v", err)
	}
	if created {
		t.Error("created should be false when port already exists")
	}
}

func TestEnsureFOU_GUEProtocolZero(t *testing.T) {
	m := newMockNetlinker()
	if _, err := EnsureFOU(m, 5555, EncapGUE); err != nil {
		t.Fatal(err)
	}
	got := m.fous[5555]
	if got.Protocol != 0 {
		t.Errorf("GUE should use Protocol=0, got %d", got.Protocol)
	}
	if got.EncapType != netlink.FOU_ENCAP_GUE {
		t.Errorf("GUE EncapType wrong: %d", got.EncapType)
	}
}
