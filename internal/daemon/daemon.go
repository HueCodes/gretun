//go:build linux

// Package daemon is the long-lived gretun client process. It holds identity,
// runs STUN, talks to the coordinator, punches holes over the disco socket,
// and orchestrates the kernel FOU+GRE data plane.
package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"time"

	"github.com/HueCodes/gretun/internal/disco"
	"github.com/HueCodes/gretun/internal/tunnel"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Config configures a daemon instance.
type Config struct {
	Coordinator string
	NodeName    string
	StateDir    string
	Iface       string // printf pattern with a single %d, e.g. "gretun%d"
	FOUPort     uint16
	DiscoAddr   string // UDP address to bind the disco socket on (e.g. ":0")
	STUNServers []string
	Aggressive  bool
	MetricsAddr string // if non-empty, expose Prometheus /metrics here
}

// Daemon is the top-level runtime. One per process.
type Daemon struct {
	cfg     Config
	nl      tunnel.Netlinker
	node    disco.NodeKey
	disco   disco.DiscoKey
	client  *disco.CoordClient
	discoCn net.PacketConn
	metrics *Metrics

	mu       sync.Mutex
	peers    map[[32]byte]*peerFSM // keyed by remote disco pubkey
	self     netip.Addr
	ifaceSeq int
	fouOwned bool
}

// New constructs a daemon. The caller still has to call Run.
func New(cfg Config, nl tunnel.Netlinker, nk disco.NodeKey, dk disco.DiscoKey) *Daemon {
	if cfg.Iface == "" {
		cfg.Iface = "gretun%d"
	}
	return &Daemon{
		cfg:    cfg,
		nl:     nl,
		node:   nk,
		disco:  dk,
		client: disco.NewCoordClient(cfg.Coordinator, nk, dk),
		peers:  make(map[[32]byte]*peerFSM),
	}
}

// Run blocks until ctx fires. It brings up the disco socket, registers with
// the coordinator, and orchestrates per-peer state machines.
func (d *Daemon) Run(ctx context.Context) error {
	addr := d.cfg.DiscoAddr
	if addr == "" {
		addr = ":0"
	}
	conn, err := net.ListenPacket("udp4", addr)
	if err != nil {
		return fmt.Errorf("disco listen: %w", err)
	}
	d.discoCn = conn
	defer conn.Close()

	local := conn.LocalAddr().(*net.UDPAddr)
	slog.Info("disco socket bound", "addr", local.String())

	created, err := tunnel.EnsureFOU(d.nl, d.cfg.FOUPort, tunnel.EncapFOU)
	if err != nil {
		return fmt.Errorf("FOU setup: %w", err)
	}
	d.fouOwned = created
	defer func() {
		if d.fouOwned {
			tunnel.RemoveFOU(d.nl, d.cfg.FOUPort)
		}
	}()

	endpoints, err := d.collectEndpoints(ctx, local.Port)
	if err != nil {
		slog.Warn("endpoint collection partially failed", "err", err)
	}

	cidr, _, err := d.client.Register(ctx, d.cfg.NodeName)
	if err != nil {
		return fmt.Errorf("register: %w", err)
	}
	slog.Info("registered", "coord", d.cfg.Coordinator, "tunnel_cidr", cidr)
	if prefix, err := netip.ParsePrefix(cidr); err == nil {
		d.self = prefix.Addr()
	}

	if err := d.client.PostEndpoints(ctx, endpoints); err != nil {
		slog.Warn("post endpoints failed", "err", err)
	}

	if d.cfg.MetricsAddr != "" {
		reg := prometheus.NewRegistry()
		d.metrics = NewMetrics(reg)
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
		srv := &http.Server{Addr: d.cfg.MetricsAddr, Handler: mux}
		go func() {
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Warn("metrics server", "err", err)
			}
		}()
		defer srv.Close()
		slog.Info("metrics listening", "addr", d.cfg.MetricsAddr)
	}

	errs := make(chan error, 4)
	go d.discoReadLoop(ctx, errs)
	go d.signalPullLoop(ctx, errs)
	go d.peersPollLoop(ctx, errs)
	go d.refreshLoop(ctx, local.Port, errs)
	go d.metricsUpdateLoop(ctx)

	select {
	case <-ctx.Done():
		slog.Info("shutting down daemon")
		d.shutdown()
		return nil
	case err := <-errs:
		slog.Error("daemon loop exited", "err", err)
		d.shutdown()
		return err
	}
}

func (d *Daemon) collectEndpoints(ctx context.Context, port int) ([]disco.RemoteEndpoint, error) {
	eps := make([]disco.RemoteEndpoint, 0, 8)

	ifAddrs, _ := net.InterfaceAddrs()
	for _, a := range ifAddrs {
		ipn, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		v4 := ipn.IP.To4()
		if v4 == nil || v4.IsLoopback() || !v4.IsGlobalUnicast() {
			continue
		}
		if ap, ok := netip.AddrFromSlice(v4); ok {
			eps = append(eps, disco.RemoteEndpoint{
				Addr:   netip.AddrPortFrom(ap, uint16(port)),
				Source: "local",
			})
		}
	}

	stunCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if pub, err := disco.DiscoverPublic(stunCtx, d.discoCn, d.cfg.STUNServers); err == nil {
		eps = append(eps, disco.RemoteEndpoint{Addr: pub.Addr, Source: "stun"})
	} else {
		return eps, err
	}
	return eps, nil
}

func (d *Daemon) discoReadLoop(ctx context.Context, errs chan<- error) {
	buf := make([]byte, 2048)
	for {
		if err := d.discoCn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			errs <- err
			return
		}
		n, from, err := d.discoCn.ReadFrom(buf)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			errs <- fmt.Errorf("disco read: %w", err)
			return
		}
		sender, body, err := disco.OpenEnvelope(buf[:n], d.disco)
		if err != nil {
			continue
		}
		fromAddr := from.(*net.UDPAddr)
		fromAP := netip.AddrPortFrom(mustAddrFromIP(fromAddr.IP), uint16(fromAddr.Port))
		d.mu.Lock()
		p := d.peers[sender]
		d.mu.Unlock()
		if p == nil {
			continue
		}
		p.onUDP(fromAP, body)
	}
}

func (d *Daemon) signalPullLoop(ctx context.Context, errs chan<- error) {
	for ctx.Err() == nil {
		sealedEnvs, err := d.client.PullSignals(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("pull signals", "err", err)
			time.Sleep(2 * time.Second)
			continue
		}
		for _, s := range sealedEnvs {
			sender, body, err := disco.OpenEnvelope(s, d.disco)
			if err != nil {
				continue
			}
			d.mu.Lock()
			p := d.peers[sender]
			d.mu.Unlock()
			if p == nil {
				continue
			}
			p.onSignal(body)
		}
	}
}

func (d *Daemon) peersPollLoop(ctx context.Context, errs chan<- error) {
	var etag string
	for ctx.Err() == nil {
		peers, newEtag, err := d.client.Peers(ctx, etag)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("peers poll", "err", err)
			time.Sleep(2 * time.Second)
			continue
		}
		etag = newEtag
		d.reconcilePeers(ctx, peers)
	}
}

func (d *Daemon) refreshLoop(ctx context.Context, discoPort int, errs chan<- error) {
	tick := time.NewTicker(25 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			eps, err := d.collectEndpoints(ctx, discoPort)
			if err != nil {
				slog.Warn("endpoint refresh", "err", err)
				continue
			}
			if err := d.client.PostEndpoints(ctx, eps); err != nil {
				slog.Warn("endpoint repost", "err", err)
			}
		}
	}
}

func (d *Daemon) reconcilePeers(ctx context.Context, peers []disco.RemotePeer) {
	d.mu.Lock()
	defer d.mu.Unlock()

	seen := make(map[[32]byte]bool, len(peers))
	for _, p := range peers {
		if p.DiscoKey == d.disco.Pub {
			continue
		}
		seen[p.DiscoKey] = true
		fsm, ok := d.peers[p.DiscoKey]
		if !ok {
			ifname := fmt.Sprintf(d.cfg.Iface, d.ifaceSeq)
			d.ifaceSeq++
			fsm = newPeerFSM(peerDeps{
				self:       d.disco,
				selfNode:   d.node,
				ifaceName:  ifname,
				fouPort:    d.cfg.FOUPort,
				selfTunnel: d.self,
				nl:         d.nl,
				discoCn:    d.discoCn,
				coord:      d.client,
				aggressive: d.cfg.Aggressive,
				metrics:    d.metrics,
			}, p)
			d.peers[p.DiscoKey] = fsm
			go fsm.run(ctx)
		} else {
			fsm.update(p)
		}
	}

	for k, fsm := range d.peers {
		if !seen[k] {
			fsm.stop()
			delete(d.peers, k)
		}
	}
}

// metricsUpdateLoop keeps the peer-state gauge current without plumbing state
// transitions through a pub-sub channel. It's coarse but cheap.
func (d *Daemon) metricsUpdateLoop(ctx context.Context) {
	if d.metrics == nil {
		return
	}
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			counts := map[string]int{
				"unknown": 0, "have_endpoints": 0, "punching": 0, "direct": 0, "relay": 0,
			}
			d.mu.Lock()
			for _, p := range d.peers {
				p.mu.Lock()
				counts[p.state.String()]++
				p.mu.Unlock()
			}
			d.mu.Unlock()
			for s, n := range counts {
				d.metrics.PeersByState.WithLabelValues(s).Set(float64(n))
			}
		}
	}
}

func (d *Daemon) shutdown() {
	d.mu.Lock()
	for _, p := range d.peers {
		p.stop()
	}
	d.peers = nil
	d.mu.Unlock()
}

// mustAddrFromIP is a tiny convenience; never fails for valid IPv4 bytes.
func mustAddrFromIP(ip net.IP) netip.Addr {
	v4 := ip.To4()
	if v4 != nil {
		a, _ := netip.AddrFromSlice(v4)
		return a
	}
	a, _ := netip.AddrFromSlice(ip)
	return a
}

func newTxID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
