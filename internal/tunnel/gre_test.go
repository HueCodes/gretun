//go:build linux

package tunnel

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/vishvananda/netlink"
)

func TestCreate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		setup   func(*mockNetlinker)
		wantErr string
	}{
		{
			name:    "missing name",
			cfg:     Config{LocalIP: net.IPv4(1, 2, 3, 4), RemoteIP: net.IPv4(5, 6, 7, 8)},
			wantErr: "tunnel name is required",
		},
		{
			name:    "missing local IP",
			cfg:     Config{Name: "tun0", RemoteIP: net.IPv4(5, 6, 7, 8)},
			wantErr: "local IP is required",
		},
		{
			name:    "missing remote IP",
			cfg:     Config{Name: "tun0", LocalIP: net.IPv4(1, 2, 3, 4)},
			wantErr: "remote IP is required",
		},
		{
			name: "tunnel already exists",
			cfg:  Config{Name: "tun0", LocalIP: net.IPv4(1, 2, 3, 4), RemoteIP: net.IPv4(5, 6, 7, 8)},
			setup: func(m *mockNetlinker) {
				m.links["tun0"] = greLink("tun0", net.IPv4(1, 1, 1, 1), net.IPv4(2, 2, 2, 2), 0, 64, true)
			},
			wantErr: "already exists",
		},
		{
			name: "LinkAdd fails",
			cfg:  Config{Name: "tun0", LocalIP: net.IPv4(1, 2, 3, 4), RemoteIP: net.IPv4(5, 6, 7, 8)},
			setup: func(m *mockNetlinker) {
				m.linkAddErr = fmt.Errorf("permission denied")
			},
			wantErr: "failed to create tunnel",
		},
		{
			name: "LinkSetUp fails triggers cleanup",
			cfg:  Config{Name: "tun0", LocalIP: net.IPv4(1, 2, 3, 4), RemoteIP: net.IPv4(5, 6, 7, 8)},
			setup: func(m *mockNetlinker) {
				m.linkSetUpErr = fmt.Errorf("device busy")
			},
			wantErr: "failed to bring up tunnel",
		},
		{
			name: "success with default TTL",
			cfg:  Config{Name: "tun0", LocalIP: net.IPv4(10, 0, 0, 1), RemoteIP: net.IPv4(10, 0, 0, 2)},
		},
		{
			name: "success with custom TTL and key",
			cfg:  Config{Name: "tun1", LocalIP: net.IPv4(10, 0, 0, 1), RemoteIP: net.IPv4(10, 0, 0, 2), Key: 42, TTL: 128},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMockNetlinker()
			if tt.setup != nil {
				tt.setup(m)
			}

			err := Create(context.Background(), m, tt.cfg)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !m.linkAddCalled {
				t.Error("expected LinkAdd to be called")
			}
			if !m.linkSetUpCalled {
				t.Error("expected LinkSetUp to be called")
			}
		})
	}
}

func TestCreate_CleanupOnSetUpFailure(t *testing.T) {
	m := newMockNetlinker()
	m.linkSetUpErr = fmt.Errorf("device busy")

	cfg := Config{Name: "tun0", LocalIP: net.IPv4(1, 2, 3, 4), RemoteIP: net.IPv4(5, 6, 7, 8)}
	err := Create(context.Background(), m, cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !m.linkDelCalled {
		t.Error("expected LinkDel to be called for cleanup")
	}
}

func TestDelete(t *testing.T) {
	tests := []struct {
		name    string
		tunnel  string
		setup   func(*mockNetlinker)
		wantErr string
	}{
		{
			name:    "empty name",
			tunnel:  "",
			wantErr: "tunnel name is required",
		},
		{
			name:    "tunnel not found",
			tunnel:  "tun0",
			wantErr: "not found",
		},
		{
			name:   "not a GRE tunnel",
			tunnel: "eth0",
			setup: func(m *mockNetlinker) {
				m.links["eth0"] = &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "eth0"}}
			},
			wantErr: "not a GRE tunnel",
		},
		{
			name:   "LinkDel fails",
			tunnel: "tun0",
			setup: func(m *mockNetlinker) {
				m.links["tun0"] = greLink("tun0", net.IPv4(1, 2, 3, 4), net.IPv4(5, 6, 7, 8), 0, 64, true)
				m.linkDelErr = fmt.Errorf("operation not permitted")
			},
			wantErr: "failed to delete",
		},
		{
			name:   "success",
			tunnel: "tun0",
			setup: func(m *mockNetlinker) {
				m.links["tun0"] = greLink("tun0", net.IPv4(1, 2, 3, 4), net.IPv4(5, 6, 7, 8), 0, 64, true)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMockNetlinker()
			if tt.setup != nil {
				tt.setup(m)
			}

			err := Delete(context.Background(), m, tt.tunnel)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestAssignIP(t *testing.T) {
	tests := []struct {
		name    string
		tunnel  string
		cidr    string
		setup   func(*mockNetlinker)
		wantErr string
	}{
		{
			name:    "tunnel not found",
			tunnel:  "tun0",
			cidr:    "192.168.1.1/30",
			wantErr: "not found",
		},
		{
			name:   "invalid CIDR",
			tunnel: "tun0",
			cidr:   "not-a-cidr",
			setup: func(m *mockNetlinker) {
				m.links["tun0"] = greLink("tun0", net.IPv4(1, 2, 3, 4), net.IPv4(5, 6, 7, 8), 0, 64, true)
			},
			wantErr: "invalid CIDR",
		},
		{
			name:   "AddrAdd fails",
			tunnel: "tun0",
			cidr:   "192.168.1.1/30",
			setup: func(m *mockNetlinker) {
				m.links["tun0"] = greLink("tun0", net.IPv4(1, 2, 3, 4), net.IPv4(5, 6, 7, 8), 0, 64, true)
				m.addrAddErr = fmt.Errorf("address exists")
			},
			wantErr: "failed to assign IP",
		},
		{
			name:   "success",
			tunnel: "tun0",
			cidr:   "192.168.1.1/30",
			setup: func(m *mockNetlinker) {
				m.links["tun0"] = greLink("tun0", net.IPv4(1, 2, 3, 4), net.IPv4(5, 6, 7, 8), 0, 64, true)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMockNetlinker()
			if tt.setup != nil {
				tt.setup(m)
			}

			err := AssignIP(context.Background(), m, tt.tunnel, tt.cidr)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !m.addrAddCalled {
				t.Error("expected AddrAdd to be called")
			}
		})
	}
}

func TestGet(t *testing.T) {
	tests := []struct {
		name       string
		tunnel     string
		setup      func(*mockNetlinker)
		wantErr    string
		wantStatus *Status
	}{
		{
			name:    "tunnel not found",
			tunnel:  "tun0",
			wantErr: "not found",
		},
		{
			name:   "not a GRE tunnel",
			tunnel: "eth0",
			setup: func(m *mockNetlinker) {
				m.links["eth0"] = &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "eth0"}}
			},
			wantErr: "not a GRE tunnel",
		},
		{
			name:   "success without IP",
			tunnel: "tun0",
			setup: func(m *mockNetlinker) {
				m.links["tun0"] = greLink("tun0", net.IPv4(10, 0, 0, 1), net.IPv4(10, 0, 0, 2), 42, 64, true)
			},
			wantStatus: &Status{
				Name:     "tun0",
				LocalIP:  "10.0.0.1",
				RemoteIP: "10.0.0.2",
				Key:      42,
				TTL:      64,
				Up:       true,
			},
		},
		{
			name:   "success with IP",
			tunnel: "tun0",
			setup: func(m *mockNetlinker) {
				m.links["tun0"] = greLink("tun0", net.IPv4(10, 0, 0, 1), net.IPv4(10, 0, 0, 2), 0, 64, true)
				m.addrs["tun0"] = []netlink.Addr{
					{IPNet: &net.IPNet{IP: net.IPv4(192, 168, 1, 1), Mask: net.CIDRMask(30, 32)}},
				}
			},
			wantStatus: &Status{
				Name:     "tun0",
				LocalIP:  "10.0.0.1",
				RemoteIP: "10.0.0.2",
				TTL:      64,
				Up:       true,
				TunnelIP: "192.168.1.1/30",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMockNetlinker()
			if tt.setup != nil {
				tt.setup(m)
			}

			status, err := Get(context.Background(), m, tt.tunnel)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if status.Name != tt.wantStatus.Name {
				t.Errorf("Name = %q, want %q", status.Name, tt.wantStatus.Name)
			}
			if status.LocalIP != tt.wantStatus.LocalIP {
				t.Errorf("LocalIP = %q, want %q", status.LocalIP, tt.wantStatus.LocalIP)
			}
			if status.RemoteIP != tt.wantStatus.RemoteIP {
				t.Errorf("RemoteIP = %q, want %q", status.RemoteIP, tt.wantStatus.RemoteIP)
			}
			if status.Key != tt.wantStatus.Key {
				t.Errorf("Key = %d, want %d", status.Key, tt.wantStatus.Key)
			}
			if status.TTL != tt.wantStatus.TTL {
				t.Errorf("TTL = %d, want %d", status.TTL, tt.wantStatus.TTL)
			}
			if status.Up != tt.wantStatus.Up {
				t.Errorf("Up = %v, want %v", status.Up, tt.wantStatus.Up)
			}
			if status.TunnelIP != tt.wantStatus.TunnelIP {
				t.Errorf("TunnelIP = %q, want %q", status.TunnelIP, tt.wantStatus.TunnelIP)
			}
		})
	}
}
