package tunnel

import (
	"net"

	"github.com/vishvananda/netlink"
)

// List returns all GRE tunnels on the system.
func List() ([]Status, error) {
	links, err := netlink.LinkList()
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

		// Get assigned IP if any
		addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
		if err == nil && len(addrs) > 0 {
			status.TunnelIP = addrs[0].IPNet.String()
		}

		tunnels = append(tunnels, status)
	}

	return tunnels, nil
}
