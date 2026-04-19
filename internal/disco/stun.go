// Package disco implements the pieces of the gretun control plane that have
// to run in userspace: STUN endpoint discovery, signed coordinator RPC, and
// the Tailscale-style disco envelope used to exchange endpoints and punch
// holes over consumer NAT.
package disco

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/pion/stun"
)

// DefaultSTUNServers is the fallback list used when a caller does not pass
// any server addresses. The same servers WireGuard's `wg-netmanager` and
// Tailscale's `magicsock` default to.
var DefaultSTUNServers = []string{
	"stun.l.google.com:19302",
	"stun1.l.google.com:19302",
	"stun.cloudflare.com:3478",
}

// stunReadTimeout caps how long we wait for any single response. The top-level
// ctx still governs overall cancellation; this just prevents one stuck server
// from stalling the read loop.
const stunReadTimeout = 3 * time.Second

// PublicEndpoint is what a STUN server reported our mapped address to be.
type PublicEndpoint struct {
	Addr netip.AddrPort
	Via  string
}

// DiscoverPublic issues a STUN Binding request to each server in parallel on
// the given UDP PacketConn and returns the first successful XOR-MAPPED-ADDRESS
// response.
//
// The conn is intentionally NOT closed. The caller keeps ownership so
// subsequent traffic rides the same NAT mapping — that's the whole point of
// STUN for hole punching.
func DiscoverPublic(ctx context.Context, conn net.PacketConn, servers []string) (PublicEndpoint, error) {
	if len(servers) == 0 {
		servers = DefaultSTUNServers
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// One outstanding request per server; txID → server address string used to
	// label the winning response.
	pending := make(map[[stun.TransactionIDSize]byte]string, len(servers))
	req := stun.MustBuild(stun.TransactionID, stun.BindingRequest)

	var sendErrs []error
	for _, s := range servers {
		raddr, err := net.ResolveUDPAddr("udp4", s)
		if err != nil {
			sendErrs = append(sendErrs, fmt.Errorf("resolve %s: %w", s, err))
			continue
		}
		// Re-roll the transaction ID for each server so we can tell responses
		// apart even when a malicious server mirrors another's txID.
		if err := req.NewTransactionID(); err != nil {
			sendErrs = append(sendErrs, fmt.Errorf("new txid: %w", err))
			continue
		}
		req.Encode()
		if _, err := conn.WriteTo(req.Raw, raddr); err != nil {
			sendErrs = append(sendErrs, fmt.Errorf("write %s: %w", s, err))
			continue
		}
		pending[req.TransactionID] = s
	}

	if len(pending) == 0 {
		return PublicEndpoint{}, fmt.Errorf("all STUN servers unreachable: %w", errors.Join(sendErrs...))
	}

	type result struct {
		ep  PublicEndpoint
		err error
	}
	resCh := make(chan result, 1)

	go func() {
		buf := make([]byte, 1500)
		for {
			if err := conn.SetReadDeadline(time.Now().Add(stunReadTimeout)); err != nil {
				resCh <- result{err: fmt.Errorf("set read deadline: %w", err)}
				return
			}
			n, _, err := conn.ReadFrom(buf)
			if err != nil {
				resCh <- result{err: fmt.Errorf("stun read: %w", err)}
				return
			}
			if ctx.Err() != nil {
				return
			}

			if !stun.IsMessage(buf[:n]) {
				continue
			}
			msg := &stun.Message{Raw: append([]byte{}, buf[:n]...)}
			if err := msg.Decode(); err != nil {
				continue
			}
			via, ok := pending[msg.TransactionID]
			if !ok {
				continue
			}

			var xor stun.XORMappedAddress
			if err := xor.GetFrom(msg); err != nil {
				continue
			}
			ip, ok := netip.AddrFromSlice(xor.IP.To4())
			if !ok {
				continue
			}
			resCh <- result{
				ep: PublicEndpoint{
					Addr: netip.AddrPortFrom(ip, uint16(xor.Port)),
					Via:  via,
				},
			}
			return
		}
	}()

	// Clear the deadline on return so the caller inherits a fresh conn.
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()

	select {
	case <-ctx.Done():
		return PublicEndpoint{}, ctx.Err()
	case r := <-resCh:
		return r.ep, r.err
	}
}
