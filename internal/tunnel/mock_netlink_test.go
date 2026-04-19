//go:build linux

package tunnel

import (
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
)

type mockNetlinker struct {
	links map[string]netlink.Link
	addrs map[string][]netlink.Addr
	fous  map[int]netlink.Fou

	linkAddErr    error
	linkDelErr    error
	linkSetUpErr  error
	linkSetMTUErr error
	linkListErr   error
	addrAddErr    error
	addrListErr   error
	fouAddErr     error
	fouDelErr     error
	fouListErr    error

	linkAddCalled    bool
	linkDelCalled    bool
	linkSetUpCalled  bool
	linkSetMTUCalled bool
	addrAddCalled    bool
	fouAddCalls      int
	fouDelCalls      int
	lastMTU          int
}

func newMockNetlinker() *mockNetlinker {
	return &mockNetlinker{
		links: make(map[string]netlink.Link),
		addrs: make(map[string][]netlink.Addr),
		fous:  make(map[int]netlink.Fou),
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
	attrs := link.Attrs()
	attrs.Flags |= net.FlagUp
	return nil
}

func (m *mockNetlinker) LinkSetMTU(link netlink.Link, mtu int) error {
	m.linkSetMTUCalled = true
	m.lastMTU = mtu
	if m.linkSetMTUErr != nil {
		return m.linkSetMTUErr
	}
	link.Attrs().MTU = mtu
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

func (m *mockNetlinker) FouAdd(fou netlink.Fou) error {
	m.fouAddCalls++
	if m.fouAddErr != nil {
		return m.fouAddErr
	}
	m.fous[fou.Port] = fou
	return nil
}

func (m *mockNetlinker) FouDel(fou netlink.Fou) error {
	m.fouDelCalls++
	if m.fouDelErr != nil {
		return m.fouDelErr
	}
	delete(m.fous, fou.Port)
	return nil
}

func (m *mockNetlinker) FouList(family int) ([]netlink.Fou, error) {
	if m.fouListErr != nil {
		return nil, m.fouListErr
	}
	var out []netlink.Fou
	for _, f := range m.fous {
		if family == 0 || f.Family == family {
			out = append(out, f)
		}
	}
	return out, nil
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
