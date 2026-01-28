package tunnel

import "net"

// Config holds the configuration for a GRE tunnel.
type Config struct {
	Name     string
	LocalIP  net.IP
	RemoteIP net.IP
	Key      uint32
	TTL      uint8
}

// Status represents the current state of a GRE tunnel.
type Status struct {
	Name     string `json:"name"`
	LocalIP  string `json:"local_ip"`
	RemoteIP string `json:"remote_ip"`
	Key      uint32 `json:"key,omitempty"`
	TTL      uint8  `json:"ttl"`
	Up       bool   `json:"up"`
	TunnelIP string `json:"tunnel_ip,omitempty"`
}
