//go:build linux

package tunnel

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"github.com/vishvananda/netlink"
)

const defaultTTL = 64

// Create creates a new GRE tunnel with the given configuration.
func Create(ctx context.Context, nl Netlinker, cfg Config) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if err := ValidateConfig(cfg); err != nil {
		return err
	}

	if _, err := nl.LinkByName(cfg.Name); err == nil {
		return &TunnelExistsError{Name: cfg.Name}
	}

	ttl := cfg.TTL
	if ttl == 0 {
		ttl = defaultTTL
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

	if err := nl.LinkAdd(gre); err != nil {
		return TranslateNetlinkError(err, "create", cfg.Name)
	}

	if err := nl.LinkSetUp(gre); err != nil {
		if delErr := nl.LinkDel(gre); delErr != nil {
			slog.Warn("failed to clean up tunnel after LinkSetUp error",
				"tunnel", cfg.Name, "error", delErr)
		}
		return TranslateNetlinkError(err, "create", cfg.Name)
	}

	slog.Info("created tunnel", "name", cfg.Name,
		"local", cfg.LocalIP, "remote", cfg.RemoteIP)

	return nil
}

// Delete removes a GRE tunnel by name.
func Delete(ctx context.Context, nl Netlinker, name string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if err := ValidateTunnelName(name); err != nil {
		return err
	}

	link, err := nl.LinkByName(name)
	if err != nil {
		return &TunnelNotFoundError{Name: name}
	}

	if link.Type() != "gre" {
		return &InvalidTypeError{
			Name:       name,
			ActualType: link.Type(),
		}
	}

	if err := nl.LinkDel(link); err != nil {
		return TranslateNetlinkError(err, "delete", name)
	}

	slog.Info("deleted tunnel", "name", name)

	return nil
}

// AssignIP assigns an IP address in CIDR notation to the tunnel interface.
func AssignIP(ctx context.Context, nl Netlinker, name string, cidr string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if err := ValidateTunnelName(name); err != nil {
		return err
	}

	if err := ValidateCIDR(cidr); err != nil {
		return err
	}

	link, err := nl.LinkByName(name)
	if err != nil {
		return &TunnelNotFoundError{Name: name}
	}

	addr, err := netlink.ParseAddr(cidr)
	if err != nil {
		return fmt.Errorf("invalid CIDR %s: %w", cidr, err)
	}

	if err := nl.AddrAdd(link, addr); err != nil {
		return TranslateNetlinkError(err, "assign-ip", name)
	}

	return nil
}

// Get retrieves the status of a specific GRE tunnel.
func Get(ctx context.Context, nl Netlinker, name string) (*Status, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	link, err := nl.LinkByName(name)
	if err != nil {
		return nil, &TunnelNotFoundError{Name: name}
	}

	gre, ok := link.(*netlink.Gretun)
	if !ok {
		return nil, &InvalidTypeError{
			Name:       name,
			ActualType: link.Type(),
		}
	}

	status := &Status{
		Name:     name,
		LocalIP:  ipToString(gre.Local),
		RemoteIP: ipToString(gre.Remote),
		Key:      gre.IKey,
		TTL:      gre.Ttl,
		Up:       link.Attrs().Flags&net.FlagUp != 0,
	}

	addrs, err := nl.AddrList(link, 0) // 0 = all address families
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
