//go:build linux

package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/HueCodes/gretun/internal/disco"
	"github.com/HueCodes/gretun/internal/tunnel"
)

// peerState is the coarse FSM state for a remote peer.
type peerState int

const (
	stateUnknown peerState = iota
	stateHaveEndpoints
	statePunching
	stateDirect
	stateRelay
)

func (s peerState) String() string {
	switch s {
	case stateUnknown:
		return "unknown"
	case stateHaveEndpoints:
		return "have_endpoints"
	case statePunching:
		return "punching"
	case stateDirect:
		return "direct"
	case stateRelay:
		return "relay"
	default:
		return fmt.Sprintf("state(%d)", int(s))
	}
}

const (
	punchInterval    = 250 * time.Millisecond
	punchAttemptDur  = 5 * time.Second
	keepaliveEvery   = 25 * time.Second
	relayRetryEvery  = 30 * time.Second
	pongMissDeadline = 75 * time.Second
)

// peerDeps is the collection of side-effect handles a peerFSM needs.
// Isolating them as an interface-free struct makes a test harness trivial.
type peerDeps struct {
	self       disco.DiscoKey
	selfNode   disco.NodeKey
	ifaceName  string
	fouPort    uint16
	selfTunnel netip.Addr
	nl         tunnel.Netlinker
	discoCn    net.PacketConn
	coord      *disco.CoordClient
	aggressive bool
}

// peerFSM owns the per-peer lifecycle.
type peerFSM struct {
	deps peerDeps

	mu        sync.Mutex
	peer      disco.RemotePeer
	state     peerState
	winning   netip.AddrPort
	tunnelUp  bool
	lastPong  time.Time
	done      chan struct{}
	incoming  chan fsmEvent
	stopOnce  sync.Once
}

type fsmEvent struct {
	kind    fsmEventKind
	addr    netip.AddrPort
	body    disco.Body
	signal  bool // true if this came via coord relay, false if direct UDP
}

type fsmEventKind int

const (
	evUpdate fsmEventKind = iota
	evUDP
	evSignal
)

func newPeerFSM(deps peerDeps, peer disco.RemotePeer) *peerFSM {
	return &peerFSM{
		deps:     deps,
		peer:     peer,
		state:    stateUnknown,
		done:     make(chan struct{}),
		incoming: make(chan fsmEvent, 32),
	}
}

// update replaces the peer snapshot (e.g. new endpoints from the coordinator).
func (p *peerFSM) update(peer disco.RemotePeer) {
	select {
	case p.incoming <- fsmEvent{kind: evUpdate}:
		p.mu.Lock()
		p.peer = peer
		p.mu.Unlock()
	default:
	}
}

// onUDP handles a disco message arriving on the disco socket.
func (p *peerFSM) onUDP(from netip.AddrPort, body disco.Body) {
	select {
	case p.incoming <- fsmEvent{kind: evUDP, addr: from, body: body}:
	default:
	}
}

// onSignal handles a disco message arriving via coord relay.
func (p *peerFSM) onSignal(body disco.Body) {
	select {
	case p.incoming <- fsmEvent{kind: evSignal, body: body, signal: true}:
	default:
	}
}

func (p *peerFSM) stop() {
	p.stopOnce.Do(func() { close(p.done) })
}

// run is the main driver goroutine.
func (p *peerFSM) run(ctx context.Context) {
	defer p.teardown()
	tick := time.NewTicker(punchInterval)
	defer tick.Stop()
	ka := time.NewTicker(keepaliveEvery)
	defer ka.Stop()
	punchDeadline := time.Time{}

	p.setState(stateUnknown)
	p.enter(stateUnknown)

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.done:
			return
		case ev := <-p.incoming:
			p.handle(ev, &punchDeadline)
		case <-tick.C:
			p.tickPunch(&punchDeadline)
		case <-ka.C:
			p.keepalive()
		}
	}
}

func (p *peerFSM) teardown() {
	p.mu.Lock()
	up := p.tunnelUp
	iface := p.deps.ifaceName
	p.mu.Unlock()
	if !up {
		return
	}
	if err := tunnel.Delete(context.Background(), p.deps.nl, iface); err != nil {
		slog.Warn("peer teardown: tunnel delete", "iface", iface, "err", err)
	}
}

func (p *peerFSM) setState(s peerState) {
	p.mu.Lock()
	prev := p.state
	p.state = s
	p.mu.Unlock()
	if prev != s {
		slog.Info("peer state change", "peer", p.peer.Name, "from", prev, "to", s)
	}
}

// enter runs one-shot actions for entering a state.
func (p *peerFSM) enter(s peerState) {
	switch s {
	case stateUnknown:
		// Advertise ourselves via call_me_maybe so the peer sends us endpoints.
		go p.sendCallMeMaybe()
	}
}

func (p *peerFSM) handle(ev fsmEvent, punchDeadline *time.Time) {
	switch ev.kind {
	case evUpdate:
		p.onPeerUpdate(punchDeadline)
	case evUDP:
		p.onDiscoUDP(ev.addr, ev.body, punchDeadline)
	case evSignal:
		p.onDiscoSignal(ev.body, punchDeadline)
	}
}

func (p *peerFSM) onPeerUpdate(punchDeadline *time.Time) {
	p.mu.Lock()
	has := len(p.peer.Endpoints) > 0
	cur := p.state
	p.mu.Unlock()

	if has && cur == stateUnknown {
		p.setState(stateHaveEndpoints)
		p.setState(statePunching)
		*punchDeadline = time.Now().Add(punchAttemptDur)
	}
}

func (p *peerFSM) onDiscoUDP(from netip.AddrPort, body disco.Body, punchDeadline *time.Time) {
	switch body.Type {
	case disco.MsgPing:
		// Reply with pong on same socket to punch in reverse.
		p.sendPong(from, body.Tx)
	case disco.MsgPong:
		// Path validated.
		p.mu.Lock()
		if p.state != stateDirect {
			p.winning = from
			p.lastPong = time.Now()
			p.mu.Unlock()
			p.setState(stateDirect)
			p.bringUpTunnel(from)
		} else {
			p.lastPong = time.Now()
			p.mu.Unlock()
		}
	case disco.MsgCallMeMaybe:
		p.absorbEndpoints(body.Endpoints)
		p.onPeerUpdate(punchDeadline)
	}
}

func (p *peerFSM) onDiscoSignal(body disco.Body, punchDeadline *time.Time) {
	// Relayed messages are typically call_me_maybe — the peer telling us
	// "here are my endpoints, try them." ping/pong over signal is rare but
	// harmless to accept.
	p.onDiscoUDP(netip.AddrPort{}, body, punchDeadline)
}

func (p *peerFSM) absorbEndpoints(addrs []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	seen := make(map[netip.AddrPort]bool)
	for _, e := range p.peer.Endpoints {
		seen[e.Addr] = true
	}
	for _, s := range addrs {
		ap, err := netip.ParseAddrPort(s)
		if err != nil || seen[ap] {
			continue
		}
		p.peer.Endpoints = append(p.peer.Endpoints, disco.RemoteEndpoint{Addr: ap, Source: "call_me_maybe"})
	}
}

func (p *peerFSM) tickPunch(punchDeadline *time.Time) {
	p.mu.Lock()
	state := p.state
	eps := append([]disco.RemoteEndpoint(nil), p.peer.Endpoints...)
	p.mu.Unlock()

	if state != statePunching {
		return
	}
	if time.Now().After(*punchDeadline) {
		p.setState(stateRelay)
		slog.Warn("hole punch failed; falling back to relay (data-plane relay not yet implemented)",
			"peer", p.peer.Name)
		return
	}
	for _, e := range eps {
		p.sendPing(e.Addr)
	}
}

func (p *peerFSM) keepalive() {
	p.mu.Lock()
	state := p.state
	peerAddr := p.winning
	last := p.lastPong
	p.mu.Unlock()
	if state != stateDirect {
		return
	}
	if time.Since(last) > pongMissDeadline {
		slog.Warn("peer keepalive lost; re-punching", "peer", p.peer.Name)
		p.setState(statePunching)
		return
	}
	p.sendPing(peerAddr)
}

func (p *peerFSM) sendPing(to netip.AddrPort) {
	body := disco.Body{
		Type:    disco.MsgPing,
		Tx:      newTxID(),
		NodeKey: p.deps.selfNode.B64(),
	}
	p.sendDisco(to, body)
}

func (p *peerFSM) sendPong(to netip.AddrPort, tx string) {
	body := disco.Body{
		Type: disco.MsgPong,
		Tx:   tx,
		Src:  to.String(),
	}
	p.sendDisco(to, body)
}

func (p *peerFSM) sendCallMeMaybe() {
	eps := gatherLocalAddrPorts(p.deps.discoCn)
	body := disco.Body{Type: disco.MsgCallMeMaybe, Endpoints: eps}
	env, err := disco.BuildEnvelope(p.deps.self, p.peer.DiscoKey, body)
	if err != nil {
		slog.Warn("build call_me_maybe", "err", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.deps.coord.SendSignal(ctx, p.peer.DiscoKey, env); err != nil {
		slog.Warn("send call_me_maybe", "peer", p.peer.Name, "err", err)
	}
}

func (p *peerFSM) sendDisco(to netip.AddrPort, body disco.Body) {
	env, err := disco.BuildEnvelope(p.deps.self, p.peer.DiscoKey, body)
	if err != nil {
		return
	}
	udp := net.UDPAddrFromAddrPort(to)
	_, _ = p.deps.discoCn.WriteTo(env, udp)
}

func (p *peerFSM) bringUpTunnel(to netip.AddrPort) {
	p.mu.Lock()
	if p.tunnelUp {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()

	// Pick local IP: first global-unicast IPv4 on any interface. This is
	// good enough for the portfolio scope; a real impl would bind to the
	// interface that carries the disco socket traffic to the peer.
	local := firstGlobalV4()
	if !local.IsValid() {
		slog.Warn("no local IPv4; cannot create tunnel")
		return
	}

	cfg := tunnel.Config{
		Name:          p.deps.ifaceName,
		LocalIP:       local.AsSlice(),
		RemoteIP:      to.Addr().AsSlice(),
		Encap:         tunnel.EncapFOU,
		EncapDport:    to.Port(),
		EncapSport:    p.deps.fouPort,
		EncapChecksum: true,
	}
	if err := tunnel.Create(context.Background(), p.deps.nl, cfg); err != nil {
		slog.Warn("tunnel create", "iface", p.deps.ifaceName, "err", err)
		return
	}
	if p.deps.selfTunnel.IsValid() && p.peer.TunnelIP.IsValid() {
		cidr := fmt.Sprintf("%s/30", p.deps.selfTunnel.String())
		_ = tunnel.AssignIP(context.Background(), p.deps.nl, p.deps.ifaceName, cidr)
	}

	p.mu.Lock()
	p.tunnelUp = true
	p.mu.Unlock()
	slog.Info("tunnel up", "iface", p.deps.ifaceName, "peer", p.peer.Name, "remote", to.String())
}

func gatherLocalAddrPorts(conn net.PacketConn) []string {
	localUDP, _ := conn.LocalAddr().(*net.UDPAddr)
	port := 0
	if localUDP != nil {
		port = localUDP.Port
	}
	var out []string
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			v4 := ipn.IP.To4()
			if v4 == nil || v4.IsLoopback() {
				continue
			}
			out = append(out, fmt.Sprintf("%s:%d", v4.String(), port))
		}
	}
	return out
}

func firstGlobalV4() netip.Addr {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return netip.Addr{}
	}
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		v4 := ipn.IP.To4()
		if v4 == nil || v4.IsLoopback() || !v4.IsGlobalUnicast() {
			continue
		}
		if ap, ok := netip.AddrFromSlice(v4); ok {
			return ap
		}
	}
	return netip.Addr{}
}
