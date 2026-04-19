//go:build linux

package tunnel

import (
	"github.com/vishvananda/netlink"
)

// Netlinker abstracts netlink operations for testability.
type Netlinker interface {
	LinkAdd(link netlink.Link) error
	LinkDel(link netlink.Link) error
	LinkByName(name string) (netlink.Link, error)
	LinkSetUp(link netlink.Link) error
	LinkSetMTU(link netlink.Link, mtu int) error
	LinkList() ([]netlink.Link, error)
	AddrAdd(link netlink.Link, addr *netlink.Addr) error
	AddrList(link netlink.Link, family int) ([]netlink.Addr, error)
	FouAdd(fou netlink.Fou) error
	FouDel(fou netlink.Fou) error
	FouList(family int) ([]netlink.Fou, error)
}

// DefaultNetlinker implements Netlinker using a single persistent netlink.Handle.
// Using a handle avoids opening and closing a new socket on every operation.
type DefaultNetlinker struct {
	handle *netlink.Handle
}

// NewDefaultNetlinker creates a DefaultNetlinker backed by a new netlink.Handle.
// The caller must call Close when the netlinker is no longer needed.
func NewDefaultNetlinker() (*DefaultNetlinker, error) {
	h, err := netlink.NewHandle()
	if err != nil {
		return nil, err
	}
	return &DefaultNetlinker{handle: h}, nil
}

// Close releases the underlying netlink socket.
func (nl *DefaultNetlinker) Close() error {
	nl.handle.Delete()
	return nil
}

// LinkAdd adds a new network link.
func (nl *DefaultNetlinker) LinkAdd(link netlink.Link) error {
	return nl.handle.LinkAdd(link)
}

// LinkDel removes a network link.
func (nl *DefaultNetlinker) LinkDel(link netlink.Link) error {
	return nl.handle.LinkDel(link)
}

// LinkByName returns the link with the given name.
func (nl *DefaultNetlinker) LinkByName(name string) (netlink.Link, error) {
	return nl.handle.LinkByName(name)
}

// LinkSetUp brings a network link up.
func (nl *DefaultNetlinker) LinkSetUp(link netlink.Link) error {
	return nl.handle.LinkSetUp(link)
}

// LinkSetMTU sets the MTU of a link.
func (nl *DefaultNetlinker) LinkSetMTU(link netlink.Link, mtu int) error {
	return nl.handle.LinkSetMTU(link, mtu)
}

// LinkList returns all network links visible to the process.
func (nl *DefaultNetlinker) LinkList() ([]netlink.Link, error) {
	return nl.handle.LinkList()
}

// AddrAdd assigns an address to a link.
func (nl *DefaultNetlinker) AddrAdd(link netlink.Link, addr *netlink.Addr) error {
	return nl.handle.AddrAdd(link, addr)
}

// AddrList returns the addresses assigned to a link for the given address family.
func (nl *DefaultNetlinker) AddrList(link netlink.Link, family int) ([]netlink.Addr, error) {
	return nl.handle.AddrList(link, family)
}

// FouAdd creates a kernel FOU (Foo-over-UDP) RX port that demuxes the given
// encapsulated IP protocol. Shared across tunnels that use the same port.
func (nl *DefaultNetlinker) FouAdd(fou netlink.Fou) error {
	return nl.handle.FouAdd(fou)
}

// FouDel removes a kernel FOU RX port.
func (nl *DefaultNetlinker) FouDel(fou netlink.Fou) error {
	return nl.handle.FouDel(fou)
}

// FouList returns the configured FOU ports for the given address family.
func (nl *DefaultNetlinker) FouList(family int) ([]netlink.Fou, error) {
	return nl.handle.FouList(family)
}
