//go:build linux

package tunnel

import (
	"context"
	"net"

	"github.com/vishvananda/netlink"
)

// List returns all GRE tunnels on the system.
func List(ctx context.Context, nl Netlinker) ([]Status, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	links, err := nl.LinkList()
	if err != nil {
		return nil, err
	}

	var tunnels []Status
	for _, link := range links {
		if link.Type() != "gre" {
			continue
		}

		gre, ok := link.(*netlink.Gretun)
		if !ok {
			continue
		}

		status := Status{
			Name:     link.Attrs().Name,
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

		tunnels = append(tunnels, status)
	}

	return tunnels, nil
}
