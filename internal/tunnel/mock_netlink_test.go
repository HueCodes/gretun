//go:build linux

package tunnel

import (
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
)

// mockNetlinker is a hand-written mock for the Netlinker interface.
type mockNetlinker struct {
	// Stored links keyed by name.
	links map[string]netlink.Link
	// Stored addresses keyed by link name.
	addrs map[string][]netlink.Addr

	// Error injection.
	linkAddErr   error
	linkDelErr   error
	linkSetUpErr error
	linkListErr  error
	addrAddErr   error
	addrListErr  error

	// Call tracking.
	linkAddCalled   bool
	linkDelCalled   bool
	linkSetUpCalled bool
	addrAddCalled   bool
}

func newMockNetlinker() *mockNetlinker {
	return &mockNetlinker{
		links: make(map[string]netlink.Link),
		addrs: make(map[string][]netlink.Addr),
	}
}

func (m *mockNetlinker) LinkAdd(link netlink.Link) error {
	m.linkAddCalled = true
	if m.linkAddErr != nil {
		return m.linkAddErr
	}
	m.links[link.Attrs().Name] = link
	return nil
}

func (m *mockNetlinker) LinkDel(link netlink.Link) error {
	m.linkDelCalled = true
	if m.linkDelErr != nil {
		return m.linkDelErr
	}
	delete(m.links, link.Attrs().Name)
	return nil
}

func (m *mockNetlinker) LinkByName(name string) (netlink.Link, error) {
	link, ok := m.links[name]
	if !ok {
		return nil, fmt.Errorf("link not found")
	}
	return link, nil
}

func (m *mockNetlinker) LinkSetUp(link netlink.Link) error {
	m.linkSetUpCalled = true
	if m.linkSetUpErr != nil {
		return m.linkSetUpErr
	}
	// Set the Up flag on the link.
	attrs := link.Attrs()
	attrs.Flags |= net.FlagUp
	return nil
}

func (m *mockNetlinker) LinkList() ([]netlink.Link, error) {
	if m.linkListErr != nil {
		return nil, m.linkListErr
	}
	var result []netlink.Link
	for _, l := range m.links {
		result = append(result, l)
	}
	return result, nil
}

func (m *mockNetlinker) AddrAdd(link netlink.Link, addr *netlink.Addr) error {
	m.addrAddCalled = true
	if m.addrAddErr != nil {
		return m.addrAddErr
	}
	name := link.Attrs().Name
	m.addrs[name] = append(m.addrs[name], *addr)
	return nil
}

func (m *mockNetlinker) AddrList(link netlink.Link, family int) ([]netlink.Addr, error) {
	if m.addrListErr != nil {
		return nil, m.addrListErr
	}
	return m.addrs[link.Attrs().Name], nil
}

// greLink creates a *netlink.Gretun for testing purposes.
func greLink(name string, local, remote net.IP, key uint32, ttl uint8, up bool) *netlink.Gretun {
	flags := net.Flags(0)
	if up {
		flags |= net.FlagUp
	}
	return &netlink.Gretun{
		LinkAttrs: netlink.LinkAttrs{
			Name:  name,
			Flags: flags,
		},
		Local:  local,
		Remote: remote,
		IKey:   key,
		OKey:   key,
		Ttl:    ttl,
	}
}
