package tunnel

import "net"

// EncapType selects the outer encapsulation used by a GRE tunnel.
type EncapType int

const (
	// EncapNone leaves the tunnel as plain GRE (IP protocol 47).
	EncapNone EncapType = iota
	// EncapFOU wraps GRE in a plain UDP header (Foo-over-UDP, RFC 8086 / LWN 614348).
	EncapFOU
	// EncapGUE wraps GRE in a GUE header (Generic UDP Encapsulation).
	EncapGUE
)

// DefaultFOUMTU is the MTU used for IPv4 FOU(+GRE) tunnels by default.
// Outer: IP(20) + UDP(8) + GRE(4) = 32 bytes; 1500 - 32 = 1468.
const DefaultFOUMTU = 1468

// Config holds the configuration for a GRE tunnel.
type Config struct {
	Name     string
	LocalIP  net.IP
	RemoteIP net.IP
	Key      uint32
	TTL      uint8
	MTU      int

	// Encapsulation. EncapNone preserves the legacy bare-GRE behaviour.
	Encap         EncapType
	EncapSport    uint16
	EncapDport    uint16
	EncapChecksum bool
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

	Encap      string `json:"encap,omitempty"`
	EncapSport uint16 `json:"encap_sport,omitempty"`
	EncapDport uint16 `json:"encap_dport,omitempty"`
	MTU        int    `json:"mtu,omitempty"`
}
