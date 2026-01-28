package tunnel

import (
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
)

// Create creates a new GRE tunnel with the given configuration.
func Create(cfg Config) error {
	if cfg.Name == "" {
		return fmt.Errorf("tunnel name is required")
	}
	if cfg.LocalIP == nil {
		return fmt.Errorf("local IP is required")
	}
	if cfg.RemoteIP == nil {
		return fmt.Errorf("remote IP is required")
	}

	// Check if tunnel already exists
	if _, err := netlink.LinkByName(cfg.Name); err == nil {
		return fmt.Errorf("tunnel %s already exists", cfg.Name)
	}

	// Set default TTL if not specified
	ttl := cfg.TTL
	if ttl == 0 {
		ttl = 64
	}

	gre := &netlink.Gretun{
		LinkAttrs: netlink.LinkAttrs{
			Name: cfg.Name,
		},
		Local:  cfg.LocalIP,
		Remote: cfg.RemoteIP,
		IKey:   cfg.Key,
		OKey:   cfg.Key,
		Ttl:    ttl,
	}

	if err := netlink.LinkAdd(gre); err != nil {
		return fmt.Errorf("failed to create tunnel %s: %w", cfg.Name, err)
	}

	if err := netlink.LinkSetUp(gre); err != nil {
		// Clean up on failure
		netlink.LinkDel(gre)
		return fmt.Errorf("failed to bring up tunnel %s: %w", cfg.Name, err)
	}

	return nil
}

// Delete removes a GRE tunnel by name.
func Delete(name string) error {
	if name == "" {
		return fmt.Errorf("tunnel name is required")
	}

	link, err := netlink.LinkByName(name)
	if err != nil {
		return fmt.Errorf("tunnel %s not found: %w", name, err)
	}

	// Verify it is a GRE tunnel
	if link.Type() != "gre" {
		return fmt.Errorf("%s is not a GRE tunnel (type: %s)", name, link.Type())
	}

	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("failed to delete tunnel %s: %w", name, err)
	}

	return nil
}

// AssignIP assigns an IP address to the tunnel interface.
func AssignIP(name string, cidr string) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return fmt.Errorf("tunnel %s not found: %w", name, err)
	}

	addr, err := netlink.ParseAddr(cidr)
	if err != nil {
		return fmt.Errorf("invalid CIDR %s: %w", cidr, err)
	}

	if err := netlink.AddrAdd(link, addr); err != nil {
		return fmt.Errorf("failed to assign IP to %s: %w", name, err)
	}

	return nil
}

// Get retrieves the status of a specific GRE tunnel.
func Get(name string) (*Status, error) {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return nil, fmt.Errorf("tunnel %s not found: %w", name, err)
	}

	gre, ok := link.(*netlink.Gretun)
	if !ok {
		return nil, fmt.Errorf("%s is not a GRE tunnel", name)
	}

	status := &Status{
		Name:     name,
		LocalIP:  ipToString(gre.Local),
		RemoteIP: ipToString(gre.Remote),
		Key:      gre.IKey,
		TTL:      gre.Ttl,
		Up:       link.Attrs().Flags&net.FlagUp != 0,
	}

	// Get assigned IP if any
	addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
	if err == nil && len(addrs) > 0 {
		status.TunnelIP = addrs[0].IPNet.String()
	}

	return status, nil
}

func ipToString(ip net.IP) string {
	if ip == nil {
		return ""
	}
	return ip.String()
}
