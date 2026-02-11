//go:build linux

package tunnel

import (
	"context"
	"fmt"
	"net"
	"testing"

	"github.com/vishvananda/netlink"
)

func TestList(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(*mockNetlinker)
		wantErr   string
		wantCount int
	}{
		{
			name: "LinkList error",
			setup: func(m *mockNetlinker) {
				m.linkListErr = fmt.Errorf("netlink error")
			},
			wantErr: "netlink error",
		},
		{
			name:      "no tunnels",
			setup:     func(m *mockNetlinker) {},
			wantCount: 0,
		},
		{
			name: "filters non-GRE links",
			setup: func(m *mockNetlinker) {
				m.links["eth0"] = &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "eth0"}}
				m.links["tun0"] = greLink("tun0", net.IPv4(10, 0, 0, 1), net.IPv4(10, 0, 0, 2), 0, 64, true)
			},
			wantCount: 1,
		},
		{
			name: "multiple GRE tunnels",
			setup: func(m *mockNetlinker) {
				m.links["tun0"] = greLink("tun0", net.IPv4(10, 0, 0, 1), net.IPv4(10, 0, 0, 2), 0, 64, true)
				m.links["tun1"] = greLink("tun1", net.IPv4(10, 0, 1, 1), net.IPv4(10, 0, 1, 2), 100, 128, false)
			},
			wantCount: 2,
		},
		{
			name: "tunnel with assigned IP",
			setup: func(m *mockNetlinker) {
				m.links["tun0"] = greLink("tun0", net.IPv4(10, 0, 0, 1), net.IPv4(10, 0, 0, 2), 0, 64, true)
				m.addrs["tun0"] = []netlink.Addr{
					{IPNet: &net.IPNet{IP: net.IPv4(192, 168, 1, 1), Mask: net.CIDRMask(30, 32)}},
				}
			},
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMockNetlinker()
			if tt.setup != nil {
				tt.setup(m)
			}

			tunnels, err := List(context.Background(), m)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(tunnels) != tt.wantCount {
				t.Errorf("got %d tunnels, want %d", len(tunnels), tt.wantCount)
			}
		})
	}
}

func TestList_TunnelIP(t *testing.T) {
	m := newMockNetlinker()
	m.links["tun0"] = greLink("tun0", net.IPv4(10, 0, 0, 1), net.IPv4(10, 0, 0, 2), 0, 64, true)
	m.addrs["tun0"] = []netlink.Addr{
		{IPNet: &net.IPNet{IP: net.IPv4(192, 168, 1, 1), Mask: net.CIDRMask(30, 32)}},
	}

	tunnels, err := List(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tunnels) != 1 {
		t.Fatalf("expected 1 tunnel, got %d", len(tunnels))
	}
	if tunnels[0].TunnelIP != "192.168.1.1/30" {
		t.Errorf("TunnelIP = %q, want %q", tunnels[0].TunnelIP, "192.168.1.1/30")
	}
}
