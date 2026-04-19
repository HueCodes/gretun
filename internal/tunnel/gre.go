//go:build linux

package tunnel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

const (
	defaultTTL = 64

	// tunnelEncapFlagCSum asks the kernel to emit UDP checksums on the outer
	// packet. Value matches TUNNEL_ENCAP_FLAG_CSUM in include/uapi/linux/ip_tunnels.h.
	tunnelEncapFlagCSum uint16 = 1
)

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

	createdFou := false
	if cfg.Encap != EncapNone {
		fou, err := ensureFOU(nl, cfg)
		if err != nil {
			return TranslateNetlinkError(err, "create", cfg.Name)
		}
		createdFou = fou
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
	applyEncap(gre, cfg)

	if err := nl.LinkAdd(gre); err != nil {
		if createdFou {
			rollbackFOU(nl, cfg)
		}
		return TranslateNetlinkError(err, "create", cfg.Name)
	}

	if mtu := mtuOrDefault(cfg); mtu > 0 {
		if err := nl.LinkSetMTU(gre, mtu); err != nil {
			if delErr := nl.LinkDel(gre); delErr != nil {
				slog.Warn("failed to clean up tunnel after LinkSetMTU error",
					"tunnel", cfg.Name, "error", delErr)
			}
			if createdFou {
				rollbackFOU(nl, cfg)
			}
			return TranslateNetlinkError(err, "create", cfg.Name)
		}
	}

	if err := nl.LinkSetUp(gre); err != nil {
		if delErr := nl.LinkDel(gre); delErr != nil {
			slog.Warn("failed to clean up tunnel after LinkSetUp error",
				"tunnel", cfg.Name, "error", delErr)
		}
		if createdFou {
			rollbackFOU(nl, cfg)
		}
		return TranslateNetlinkError(err, "create", cfg.Name)
	}

	slog.Info("created tunnel", "name", cfg.Name,
		"local", cfg.LocalIP, "remote", cfg.RemoteIP,
		"encap", encapTypeName(cfg.Encap), "encap_dport", cfg.EncapDport)

	return nil
}

// ensureFOU adds a FOU RX port if one does not already exist for (family, port, proto).
// Returns true iff this call created the port (caller must FouDel on rollback).
func ensureFOU(nl Netlinker, cfg Config) (bool, error) {
	return EnsureFOU(nl, cfg.EncapDport, cfg.Encap)
}

// EnsureFOU opens a kernel FOU RX port, tolerating an already-present port.
// Returns true iff this call created the port.
func EnsureFOU(nl Netlinker, port uint16, encap EncapType) (bool, error) {
	fou := netlink.Fou{
		Family:    unix.AF_INET,
		Port:      int(port),
		Protocol:  unix.IPPROTO_GRE,
		EncapType: fouEncapConst(encap),
	}
	if encap == EncapGUE {
		fou.Protocol = 0
	}
	if err := nl.FouAdd(fou); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// RemoveFOU drops a FOU RX port. Errors are logged but not returned since
// the port might not exist in cleanup paths.
func RemoveFOU(nl Netlinker, port uint16) {
	rollbackFOU(nl, Config{EncapDport: port})
}

func rollbackFOU(nl Netlinker, cfg Config) {
	err := nl.FouDel(netlink.Fou{
		Family: unix.AF_INET,
		Port:   int(cfg.EncapDport),
	})
	if err != nil {
		slog.Warn("failed to roll back FOU port",
			"port", cfg.EncapDport, "error", err)
	}
}

// applyEncap sets the encap fields on a Gretun. The netlink library serialises
// EncapSport/EncapDport with htons internally, so we pass native byte order here.
func applyEncap(gre *netlink.Gretun, cfg Config) {
	if cfg.Encap == EncapNone {
		return
	}
	gre.EncapType = uint16(netlinkEncapConst(cfg.Encap))
	gre.EncapSport = cfg.EncapSport
	gre.EncapDport = cfg.EncapDport
	if cfg.EncapChecksum {
		gre.EncapFlags = tunnelEncapFlagCSum
	}
}

func fouEncapConst(e EncapType) int {
	switch e {
	case EncapGUE:
		return netlink.FOU_ENCAP_GUE
	default:
		return netlink.FOU_ENCAP_DIRECT
	}
}

func netlinkEncapConst(e EncapType) int {
	switch e {
	case EncapGUE:
		return netlink.FOU_ENCAP_GUE
	default:
		return netlink.FOU_ENCAP_DIRECT
	}
}

func encapTypeName(e EncapType) string {
	switch e {
	case EncapFOU:
		return "fou"
	case EncapGUE:
		return "gue"
	default:
		return "none"
	}
}

func mtuOrDefault(cfg Config) int {
	if cfg.MTU > 0 {
		return cfg.MTU
	}
	if cfg.Encap != EncapNone {
		return DefaultFOUMTU
	}
	return 0
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

	status := statusFromGretun(link, gre)

	addrs, err := nl.AddrList(link, 0) // 0 = all address families
	if err == nil && len(addrs) > 0 {
		status.TunnelIP = addrs[0].IPNet.String()
	}

	return status, nil
}

// statusFromGretun populates a Status from a netlink.Gretun link.
func statusFromGretun(link netlink.Link, gre *netlink.Gretun) *Status {
	s := &Status{
		Name:     link.Attrs().Name,
		LocalIP:  ipToString(gre.Local),
		RemoteIP: ipToString(gre.Remote),
		Key:      gre.IKey,
		TTL:      gre.Ttl,
		Up:       link.Attrs().Flags&net.FlagUp != 0,
		MTU:      link.Attrs().MTU,
	}

	switch int(gre.EncapType) {
	case netlink.FOU_ENCAP_DIRECT:
		s.Encap = "fou"
	case netlink.FOU_ENCAP_GUE:
		s.Encap = "gue"
	}
	s.EncapSport = gre.EncapSport
	s.EncapDport = gre.EncapDport

	return s
}

func ipToString(ip net.IP) string {
	if ip == nil {
		return ""
	}
	return ip.String()
}
