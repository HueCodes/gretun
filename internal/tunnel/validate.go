//go:build linux

package tunnel

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

const (
	// maxInterfaceNameLength is the maximum number of characters allowed in a
	// Linux network interface name, derived from IFNAMSIZ (16) minus the NUL
	// terminator.
	maxInterfaceNameLength = 15
)

var (
	validInterfaceNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

	// reservedPrefixes are interface name prefixes that may conflict with system interfaces.
	reservedPrefixes = []string{
		"lo", "eth", "wlan", "docker", "veth", "br-", "virbr",
	}
)

// ValidateTunnelName validates a tunnel interface name.
func ValidateTunnelName(name string) error {
	if name == "" {
		return fmt.Errorf("tunnel name cannot be empty")
	}

	if len(name) > maxInterfaceNameLength {
		return fmt.Errorf("tunnel name %q exceeds maximum length of %d characters (got %d)",
			name, maxInterfaceNameLength, len(name))
	}

	if !validInterfaceNamePattern.MatchString(name) {
		return fmt.Errorf("tunnel name %q contains invalid characters (only alphanumeric, hyphens, and underscores allowed)",
			name)
	}

	return nil
}

// ValidateCIDR validates a CIDR notation IP address, rejecting network and broadcast addresses.
func ValidateCIDR(cidr string) error {
	if cidr == "" {
		return fmt.Errorf("CIDR cannot be empty")
	}

	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("invalid CIDR notation %q: %w", cidr, err)
	}

	if ip.To4() == nil {
		return fmt.Errorf("CIDR %q is not an IPv4 address", cidr)
	}

	ones, _ := ipNet.Mask.Size()

	// /32 has a single address — network/broadcast checks don't apply.
	if ones == 32 {
		return nil
	}

	if ip.Equal(ipNet.IP) {
		return fmt.Errorf("CIDR %q uses network address (first address in subnet), which is typically reserved",
			cidr)
	}

	broadcast := make(net.IP, len(ipNet.IP))
	copy(broadcast, ipNet.IP)
	for i := range broadcast {
		broadcast[i] |= ^ipNet.Mask[i]
	}

	if ip.Equal(broadcast) {
		return fmt.Errorf("CIDR %q uses broadcast address (last address in subnet), which is reserved",
			cidr)
	}

	return nil
}

// ValidateIP validates an IP address, rejecting loopback, unspecified, multicast, and IPv6.
func ValidateIP(ip net.IP, fieldName string) error {
	if ip == nil {
		return fmt.Errorf("%s is required", fieldName)
	}

	if ip.IsUnspecified() {
		return fmt.Errorf("%s cannot be unspecified (0.0.0.0)", fieldName)
	}

	if ip.IsLoopback() {
		return fmt.Errorf("%s cannot be loopback address (%s)", fieldName, ip.String())
	}

	if ip.To4() == nil {
		return fmt.Errorf("%s must be an IPv4 address (got %s)", fieldName, ip.String())
	}

	if ip.IsMulticast() {
		return fmt.Errorf("%s cannot be a multicast address (%s)", fieldName, ip.String())
	}

	return nil
}

// ValidateTTL validates a TTL value. Zero means "use default".
func ValidateTTL(ttl uint8) error {
	if ttl == 0 {
		return nil
	}

	if ttl < 1 {
		return fmt.Errorf("TTL must be 0 (default) or between 1-255 (got %d)", ttl)
	}

	return nil
}

// warnPortsNonFatal is a set of well-known UDP ports whose reuse is legal but
// usually a misconfiguration. We log, not fail.
var warnPortsNonFatal = map[uint16]string{
	53:   "DNS",
	67:   "DHCP server",
	68:   "DHCP client",
	123:  "NTP",
	443:  "HTTPS/QUIC",
	500:  "IKE",
	4500: "IPsec NAT-T",
}

// ValidateEncap validates the encapsulation-related fields on Config.
// It also warns (via the returned 'warn' string) for dports that collide with
// well-known services; callers may surface these warnings as they see fit.
func ValidateEncap(cfg Config) (warn string, err error) {
	switch cfg.Encap {
	case EncapNone:
		// Accept legacy zero values.
	case EncapFOU, EncapGUE:
		if cfg.EncapDport == 0 {
			return "", fmt.Errorf("encap-dport is required when encap=%s", encapTypeName(cfg.Encap))
		}
		if svc, ok := warnPortsNonFatal[cfg.EncapDport]; ok {
			warn = fmt.Sprintf("encap-dport %d collides with well-known %s; continuing", cfg.EncapDport, svc)
		}
	default:
		return "", fmt.Errorf("unknown encap type %d", cfg.Encap)
	}

	if cfg.MTU != 0 && (cfg.MTU < 576 || cfg.MTU > 9000) {
		return warn, fmt.Errorf("MTU must be 0 (default) or between 576 and 9000 (got %d)", cfg.MTU)
	}

	return warn, nil
}

// ValidateConfig performs comprehensive validation on a tunnel configuration.
func ValidateConfig(cfg Config) error {
	if err := ValidateTunnelName(cfg.Name); err != nil {
		return err
	}

	for _, prefix := range reservedPrefixes {
		if strings.HasPrefix(cfg.Name, prefix) {
			return fmt.Errorf("tunnel name %q uses reserved prefix %q which may conflict with system interfaces",
				cfg.Name, prefix)
		}
	}

	if err := ValidateIP(cfg.LocalIP, "local IP"); err != nil {
		return err
	}

	if err := ValidateIP(cfg.RemoteIP, "remote IP"); err != nil {
		return err
	}

	if cfg.LocalIP.Equal(cfg.RemoteIP) {
		return fmt.Errorf("local IP and remote IP cannot be the same (%s)", cfg.LocalIP.String())
	}

	if err := ValidateTTL(cfg.TTL); err != nil {
		return err
	}

	if _, err := ValidateEncap(cfg); err != nil {
		return err
	}

	return nil
}
