//go:build linux

package tunnel

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

const (
	// Linux interface name max length (IFNAMSIZ - 1)
	maxInterfaceNameLength = 15
)

var (
	// Valid interface name pattern: alphanumeric, hyphens, underscores
	validInterfaceNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

	// Reserved interface name prefixes that should not be used
	reservedPrefixes = []string{
		"lo",      // loopback
		"eth",     // ethernet (commonly used by system)
		"wlan",    // wireless
		"docker",  // docker interfaces
		"veth",    // virtual ethernet
		"br-",     // bridge
		"virbr",   // virtual bridge
	}
)

// ValidateTunnelName validates a tunnel interface name.
// Returns an error if the name is invalid.
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

	// Check for reserved prefixes (warn, don't error)
	for _, prefix := range reservedPrefixes {
		if strings.HasPrefix(name, prefix) {
			// This is a warning, but we'll allow it - just make it clear in the message
			// that it might conflict with system interfaces
			return fmt.Errorf("tunnel name %q uses reserved prefix %q which may conflict with system interfaces",
				name, prefix)
		}
	}

	return nil
}

// ValidateCIDR validates a CIDR notation IP address.
// Returns an error if the CIDR is invalid or uses network/broadcast addresses.
func ValidateCIDR(cidr string) error {
	if cidr == "" {
		return fmt.Errorf("CIDR cannot be empty")
	}

	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("invalid CIDR notation %q: %w", cidr, err)
	}

	// Check if it's IPv4 (GRE tunnels typically use IPv4)
	if ip.To4() == nil {
		return fmt.Errorf("CIDR %q is not an IPv4 address", cidr)
	}

	// Check if IP is the network address (first address in the subnet)
	if ip.Equal(ipNet.IP) {
		return fmt.Errorf("CIDR %q uses network address (first address in subnet), which is typically reserved",
			cidr)
	}

	// Calculate broadcast address
	broadcast := make(net.IP, len(ipNet.IP))
	copy(broadcast, ipNet.IP)
	for i := range broadcast {
		broadcast[i] |= ^ipNet.Mask[i]
	}

	// Check if IP is the broadcast address (last address in the subnet)
	if ip.Equal(broadcast) {
		return fmt.Errorf("CIDR %q uses broadcast address (last address in subnet), which is reserved",
			cidr)
	}

	return nil
}

// ValidateIP validates an IP address.
// Returns an error if the IP is invalid, loopback, or unspecified.
func ValidateIP(ip net.IP, fieldName string) error {
	if ip == nil {
		return fmt.Errorf("%s is required", fieldName)
	}

	// Check for unspecified address (0.0.0.0 or ::)
	if ip.IsUnspecified() {
		return fmt.Errorf("%s cannot be unspecified (0.0.0.0)", fieldName)
	}

	// Check for loopback (127.0.0.0/8 or ::1)
	if ip.IsLoopback() {
		return fmt.Errorf("%s cannot be loopback address (%s)", fieldName, ip.String())
	}

	// Check if it's IPv4
	if ip.To4() == nil {
		return fmt.Errorf("%s must be an IPv4 address (got %s)", fieldName, ip.String())
	}

	// Check for multicast addresses
	if ip.IsMulticast() {
		return fmt.Errorf("%s cannot be a multicast address (%s)", fieldName, ip.String())
	}

	return nil
}

// ValidateTTL validates a TTL value.
// TTL of 0 is allowed (means use default), otherwise must be 1-255.
func ValidateTTL(ttl uint8) error {
	// TTL of 0 is valid (it means use the default)
	if ttl == 0 {
		return nil
	}

	// TTL must be at least 1 if specified
	// Note: uint8 max is 255, so we don't need to check upper bound
	if ttl < 1 {
		return fmt.Errorf("TTL must be 0 (default) or between 1-255 (got %d)", ttl)
	}

	return nil
}

// ValidateConfig performs comprehensive validation on a tunnel configuration.
// This is a convenience function that validates all fields.
func ValidateConfig(cfg Config) error {
	if err := ValidateTunnelName(cfg.Name); err != nil {
		return err
	}

	if err := ValidateIP(cfg.LocalIP, "local IP"); err != nil {
		return err
	}

	if err := ValidateIP(cfg.RemoteIP, "remote IP"); err != nil {
		return err
	}

	// Check that local and remote IPs are different
	if cfg.LocalIP.Equal(cfg.RemoteIP) {
		return fmt.Errorf("local IP and remote IP cannot be the same (%s)", cfg.LocalIP.String())
	}

	if err := ValidateTTL(cfg.TTL); err != nil {
		return err
	}

	return nil
}
