//go:build linux

package tunnel

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func fouCfg(name string, dport uint16) Config {
	return Config{
		Name:          name,
		LocalIP:       net.IPv4(10, 0, 0, 1),
		RemoteIP:      net.IPv4(10, 0, 0, 2),
		Encap:         EncapFOU,
		EncapDport:    dport,
		EncapChecksum: true,
	}
}

func TestCreate_FOU(t *testing.T) {
	m := newMockNetlinker()
	cfg := fouCfg("tun0", 7777)

	if err := Create(context.Background(), m, cfg); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if m.fouAddCalls != 1 {
		t.Errorf("FouAdd calls = %d, want 1", m.fouAddCalls)
	}
	fou, ok := m.fous[7777]
	if !ok {
		t.Fatal("expected FOU port 7777 in state")
	}
	if fou.Family != unix.AF_INET {
		t.Errorf("FOU Family = %d, want AF_INET", fou.Family)
	}
	if fou.Protocol != unix.IPPROTO_GRE {
		t.Errorf("FOU Protocol = %d, want IPPROTO_GRE", fou.Protocol)
	}
	if fou.EncapType != netlink.FOU_ENCAP_DIRECT {
		t.Errorf("FOU EncapType = %d, want FOU_ENCAP_DIRECT", fou.EncapType)
	}

	link, ok := m.links["tun0"].(*netlink.Gretun)
	if !ok {
		t.Fatal("expected Gretun link")
	}
	if link.EncapType != uint16(netlink.FOU_ENCAP_DIRECT) {
		t.Errorf("link EncapType = %d, want FOU_ENCAP_DIRECT", link.EncapType)
	}
	if link.EncapDport != 7777 {
		t.Errorf("link EncapDport = %d, want 7777", link.EncapDport)
	}
	if link.EncapFlags != tunnelEncapFlagCSum {
		t.Errorf("link EncapFlags = %d, want TUNNEL_ENCAP_FLAG_CSUM", link.EncapFlags)
	}
	if !m.linkSetMTUCalled || m.lastMTU != DefaultFOUMTU {
		t.Errorf("MTU setup = called:%v mtu:%d, want called:true mtu:%d",
			m.linkSetMTUCalled, m.lastMTU, DefaultFOUMTU)
	}
}

func TestCreate_FOU_PortReuse(t *testing.T) {
	m := newMockNetlinker()
	if err := Create(context.Background(), m, fouCfg("tun0", 7777)); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	// Second call: simulate the kernel returning EEXIST because the FOU port
	// already exists. FouAdd must be called, but the port must be treated as
	// pre-existing (no rollback delete if the later LinkAdd succeeds).
	m.fouAddErr = unix.EEXIST
	if err := Create(context.Background(), m, fouCfg("tun1", 7777)); err != nil {
		t.Fatalf("second Create: %v", err)
	}
	if m.fouDelCalls != 0 {
		t.Errorf("unexpected FouDel calls: %d", m.fouDelCalls)
	}
	if _, ok := m.links["tun1"]; !ok {
		t.Error("tun1 was not created")
	}
}

func TestCreate_FOU_Rollback_OnLinkSetUpFailure(t *testing.T) {
	m := newMockNetlinker()
	m.linkSetUpErr = fmt.Errorf("device busy")

	err := Create(context.Background(), m, fouCfg("tun0", 7777))
	if err == nil {
		t.Fatal("expected error from LinkSetUp failure")
	}
	if !m.linkDelCalled {
		t.Error("expected LinkDel during rollback")
	}
	if m.fouDelCalls != 1 {
		t.Errorf("expected 1 FouDel during rollback, got %d", m.fouDelCalls)
	}
	if _, ok := m.fous[7777]; ok {
		t.Error("FOU port should have been rolled back")
	}
}

func TestCreate_FOU_Rollback_PreExistingFOU(t *testing.T) {
	m := newMockNetlinker()
	// Pretend the FOU already exists (simulated by having FouAdd return EEXIST).
	m.fouAddErr = unix.EEXIST
	m.linkSetUpErr = fmt.Errorf("device busy")

	err := Create(context.Background(), m, fouCfg("tun0", 7777))
	if err == nil {
		t.Fatal("expected error")
	}
	if m.fouDelCalls != 0 {
		t.Errorf("pre-existing FOU should not be deleted on rollback, got %d FouDel calls",
			m.fouDelCalls)
	}
	if !m.linkDelCalled {
		t.Error("expected LinkDel during rollback")
	}
}

func TestCreate_FOU_Rollback_OnLinkAddFailure(t *testing.T) {
	m := newMockNetlinker()
	m.linkAddErr = fmt.Errorf("permission denied")

	err := Create(context.Background(), m, fouCfg("tun0", 7777))
	if err == nil {
		t.Fatal("expected error")
	}
	if m.fouDelCalls != 1 {
		t.Errorf("expected 1 FouDel during rollback, got %d", m.fouDelCalls)
	}
}

func TestValidateEncap(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name: "none passes",
			cfg:  Config{Name: "tun0", LocalIP: net.IPv4(1, 2, 3, 4), RemoteIP: net.IPv4(5, 6, 7, 8)},
		},
		{
			name: "fou without dport",
			cfg: Config{
				Name: "tun0", LocalIP: net.IPv4(1, 2, 3, 4), RemoteIP: net.IPv4(5, 6, 7, 8),
				Encap: EncapFOU,
			},
			wantErr: "encap-dport is required",
		},
		{
			name: "invalid encap type",
			cfg: Config{
				Name: "tun0", LocalIP: net.IPv4(1, 2, 3, 4), RemoteIP: net.IPv4(5, 6, 7, 8),
				Encap: EncapType(42), EncapDport: 7777,
			},
			wantErr: "unknown encap type",
		},
		{
			name: "MTU too small",
			cfg: Config{
				Name: "tun0", LocalIP: net.IPv4(1, 2, 3, 4), RemoteIP: net.IPv4(5, 6, 7, 8),
				MTU: 100,
			},
			wantErr: "MTU must be",
		},
		{
			name: "MTU too large",
			cfg: Config{
				Name: "tun0", LocalIP: net.IPv4(1, 2, 3, 4), RemoteIP: net.IPv4(5, 6, 7, 8),
				MTU: 20000,
			},
			wantErr: "MTU must be",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidateEncap(tt.cfg)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestValidateEncap_WarnsOnWellKnownPort(t *testing.T) {
	cfg := Config{
		Name: "tun0", LocalIP: net.IPv4(1, 2, 3, 4), RemoteIP: net.IPv4(5, 6, 7, 8),
		Encap: EncapFOU, EncapDport: 443,
	}
	warn, err := ValidateEncap(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(warn, "443") {
		t.Errorf("want warning about port 443, got %q", warn)
	}
}
