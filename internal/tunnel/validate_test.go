//go:build linux

package tunnel

import (
	"net"
	"strings"
	"testing"
)

func TestValidateTunnelName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid name",
			input:   "gre0",
			wantErr: false,
		},
		{
			name:    "valid name with hyphen",
			input:   "gre-tunnel-1",
			wantErr: false,
		},
		{
			name:    "valid name with underscore",
			input:   "gre_tunnel_1",
			wantErr: false,
		},
		{
			name:    "empty name",
			input:   "",
			wantErr: true,
			errMsg:  "cannot be empty",
		},
		{
			name:    "name too long",
			input:   "verylongtunnelname123",
			wantErr: true,
			errMsg:  "exceeds maximum length",
		},
		{
			name:    "invalid characters - spaces",
			input:   "gre tunnel",
			wantErr: true,
			errMsg:  "invalid characters",
		},
		{
			name:    "invalid characters - special chars",
			input:   "gre@tunnel",
			wantErr: true,
			errMsg:  "invalid characters",
		},
		{
			name:    "invalid characters - dot",
			input:   "gre.tunnel",
			wantErr: true,
			errMsg:  "invalid characters",
		},
		{
			name:    "reserved prefix - lo (allowed in name validation)",
			input:   "lo0",
			wantErr: false,
		},
		{
			name:    "reserved prefix - eth (allowed in name validation)",
			input:   "eth99",
			wantErr: false,
		},
		{
			name:    "reserved prefix - docker (allowed in name validation)",
			input:   "docker0",
			wantErr: false,
		},
		{
			name:    "reserved prefix - br- (allowed in name validation)",
			input:   "br-12345",
			wantErr: false,
		},
		{
			name:    "max length valid",
			input:   "gretunnel123456",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTunnelName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateTunnelName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("ValidateTunnelName(%q) error = %v, want error containing %q", tt.input, err, tt.errMsg)
			}
		})
	}
}

func TestValidateCIDR(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid CIDR",
			input:   "10.0.0.1/24",
			wantErr: false,
		},
		{
			name:    "valid CIDR /32",
			input:   "192.168.1.100/32",
			wantErr: false,
		},
		{
			name:    "valid CIDR /16",
			input:   "172.16.0.10/16",
			wantErr: false,
		},
		{
			name:    "empty CIDR",
			input:   "",
			wantErr: true,
			errMsg:  "cannot be empty",
		},
		{
			name:    "invalid CIDR - no prefix",
			input:   "10.0.0.1",
			wantErr: true,
			errMsg:  "invalid CIDR notation",
		},
		{
			name:    "invalid CIDR - bad IP",
			input:   "999.999.999.999/24",
			wantErr: true,
			errMsg:  "invalid CIDR notation",
		},
		{
			name:    "invalid CIDR - bad prefix",
			input:   "10.0.0.1/99",
			wantErr: true,
			errMsg:  "invalid CIDR notation",
		},
		{
			name:    "network address",
			input:   "10.0.0.0/24",
			wantErr: true,
			errMsg:  "network address",
		},
		{
			name:    "broadcast address",
			input:   "10.0.0.255/24",
			wantErr: true,
			errMsg:  "broadcast address",
		},
		{
			name:    "IPv6 address",
			input:   "fe80::1/64",
			wantErr: true,
			errMsg:  "not an IPv4 address",
		},
		{
			name:    "network address /16",
			input:   "172.16.0.0/16",
			wantErr: true,
			errMsg:  "network address",
		},
		{
			name:    "broadcast address /16",
			input:   "172.16.255.255/16",
			wantErr: true,
			errMsg:  "broadcast address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCIDR(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCIDR(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("ValidateCIDR(%q) error = %v, want error containing %q", tt.input, err, tt.errMsg)
			}
		})
	}
}

func TestValidateIP(t *testing.T) {
	tests := []struct {
		name      string
		ip        net.IP
		fieldName string
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "valid IPv4",
			ip:        net.ParseIP("10.0.0.1"),
			fieldName: "test IP",
			wantErr:   false,
		},
		{
			name:      "valid public IPv4",
			ip:        net.ParseIP("8.8.8.8"),
			fieldName: "test IP",
			wantErr:   false,
		},
		{
			name:      "nil IP",
			ip:        nil,
			fieldName: "test IP",
			wantErr:   true,
			errMsg:    "is required",
		},
		{
			name:      "unspecified IPv4",
			ip:        net.ParseIP("0.0.0.0"),
			fieldName: "test IP",
			wantErr:   true,
			errMsg:    "cannot be unspecified",
		},
		{
			name:      "loopback IPv4",
			ip:        net.ParseIP("127.0.0.1"),
			fieldName: "test IP",
			wantErr:   true,
			errMsg:    "cannot be loopback",
		},
		{
			name:      "loopback IPv4 range",
			ip:        net.ParseIP("127.0.0.100"),
			fieldName: "test IP",
			wantErr:   true,
			errMsg:    "cannot be loopback",
		},
		{
			name:      "IPv6 address",
			ip:        net.ParseIP("fe80::1"),
			fieldName: "test IP",
			wantErr:   true,
			errMsg:    "must be an IPv4 address",
		},
		{
			name:      "multicast IPv4",
			ip:        net.ParseIP("224.0.0.1"),
			fieldName: "test IP",
			wantErr:   true,
			errMsg:    "cannot be a multicast",
		},
		{
			name:      "multicast IPv4 range",
			ip:        net.ParseIP("239.255.255.255"),
			fieldName: "test IP",
			wantErr:   true,
			errMsg:    "cannot be a multicast",
		},
		{
			name:      "private IPv4",
			ip:        net.ParseIP("192.168.1.1"),
			fieldName: "test IP",
			wantErr:   false,
		},
		{
			name:      "custom field name in error",
			ip:        nil,
			fieldName: "local IP",
			wantErr:   true,
			errMsg:    "local IP is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateIP(tt.ip, tt.fieldName)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateIP(%v, %q) error = %v, wantErr %v", tt.ip, tt.fieldName, err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("ValidateIP(%v, %q) error = %v, want error containing %q", tt.ip, tt.fieldName, err, tt.errMsg)
			}
		})
	}
}

func TestValidateTTL(t *testing.T) {
	tests := []struct {
		name    string
		ttl     uint8
		wantErr bool
		errMsg  string
	}{
		{
			name:    "default TTL (0)",
			ttl:     0,
			wantErr: false,
		},
		{
			name:    "minimum valid TTL",
			ttl:     1,
			wantErr: false,
		},
		{
			name:    "typical TTL",
			ttl:     64,
			wantErr: false,
		},
		{
			name:    "maximum TTL",
			ttl:     255,
			wantErr: false,
		},
		{
			name:    "common TTL",
			ttl:     128,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTTL(tt.ttl)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateTTL(%d) error = %v, wantErr %v", tt.ttl, err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("ValidateTTL(%d) error = %v, want error containing %q", tt.ttl, err, tt.errMsg)
			}
		})
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			cfg: Config{
				Name:     "gre0",
				LocalIP:  net.ParseIP("10.0.0.1"),
				RemoteIP: net.ParseIP("10.0.0.2"),
				TTL:      64,
			},
			wantErr: false,
		},
		{
			name: "valid config with default TTL",
			cfg: Config{
				Name:     "gre0",
				LocalIP:  net.ParseIP("10.0.0.1"),
				RemoteIP: net.ParseIP("10.0.0.2"),
				TTL:      0,
			},
			wantErr: false,
		},
		{
			name: "invalid tunnel name",
			cfg: Config{
				Name:     "",
				LocalIP:  net.ParseIP("10.0.0.1"),
				RemoteIP: net.ParseIP("10.0.0.2"),
			},
			wantErr: true,
			errMsg:  "cannot be empty",
		},
		{
			name: "nil local IP",
			cfg: Config{
				Name:     "gre0",
				LocalIP:  nil,
				RemoteIP: net.ParseIP("10.0.0.2"),
			},
			wantErr: true,
			errMsg:  "local IP is required",
		},
		{
			name: "nil remote IP",
			cfg: Config{
				Name:     "gre0",
				LocalIP:  net.ParseIP("10.0.0.1"),
				RemoteIP: nil,
			},
			wantErr: true,
			errMsg:  "remote IP is required",
		},
		{
			name: "same local and remote IP",
			cfg: Config{
				Name:     "gre0",
				LocalIP:  net.ParseIP("10.0.0.1"),
				RemoteIP: net.ParseIP("10.0.0.1"),
			},
			wantErr: true,
			errMsg:  "cannot be the same",
		},
		{
			name: "loopback local IP",
			cfg: Config{
				Name:     "gre0",
				LocalIP:  net.ParseIP("127.0.0.1"),
				RemoteIP: net.ParseIP("10.0.0.2"),
			},
			wantErr: true,
			errMsg:  "cannot be loopback",
		},
		{
			name: "multicast remote IP",
			cfg: Config{
				Name:     "gre0",
				LocalIP:  net.ParseIP("10.0.0.1"),
				RemoteIP: net.ParseIP("224.0.0.1"),
			},
			wantErr: true,
			errMsg:  "cannot be a multicast",
		},
		{
			name: "reserved prefix eth",
			cfg: Config{
				Name:     "eth99",
				LocalIP:  net.ParseIP("10.0.0.1"),
				RemoteIP: net.ParseIP("10.0.0.2"),
			},
			wantErr: true,
			errMsg:  "reserved prefix",
		},
		{
			name: "reserved prefix docker",
			cfg: Config{
				Name:     "docker0",
				LocalIP:  net.ParseIP("10.0.0.1"),
				RemoteIP: net.ParseIP("10.0.0.2"),
			},
			wantErr: true,
			errMsg:  "reserved prefix",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("ValidateConfig() error = %v, want error containing %q", err, tt.errMsg)
			}
		})
	}
}
