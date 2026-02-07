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
	LinkList() ([]netlink.Link, error)
	AddrAdd(link netlink.Link, addr *netlink.Addr) error
	AddrList(link netlink.Link, family int) ([]netlink.Addr, error)
}

// DefaultNetlinker delegates to the real netlink package.
type DefaultNetlinker struct{}

func (DefaultNetlinker) LinkAdd(link netlink.Link) error            { return netlink.LinkAdd(link) }
func (DefaultNetlinker) LinkDel(link netlink.Link) error            { return netlink.LinkDel(link) }
func (DefaultNetlinker) LinkByName(name string) (netlink.Link, error) { return netlink.LinkByName(name) }
func (DefaultNetlinker) LinkSetUp(link netlink.Link) error          { return netlink.LinkSetUp(link) }
func (DefaultNetlinker) LinkList() ([]netlink.Link, error)          { return netlink.LinkList() }
func (DefaultNetlinker) AddrAdd(link netlink.Link, addr *netlink.Addr) error {
	return netlink.AddrAdd(link, addr)
}
func (DefaultNetlinker) AddrList(link netlink.Link, family int) ([]netlink.Addr, error) {
	return netlink.AddrList(link, family)
}
