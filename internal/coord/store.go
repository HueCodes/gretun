package coord

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"sync"
	"time"
)

// Store is the coordinator's state interface. Implementations must be safe
// for concurrent use.
type Store interface {
	Register(ctx context.Context, p Peer) (netip.Addr, error)
	SetEndpoints(ctx context.Context, nodeKey ed25519.PublicKey, eps []Endpoint) error
	Peers(ctx context.Context) ([]Peer, string, error)
	WaitForPeersChange(ctx context.Context, since string) error

	EnqueueSignal(ctx context.Context, to [32]byte, env Envelope) error
	PopSignals(ctx context.Context, to [32]byte) ([]Envelope, error)
	WaitForSignal(ctx context.Context, to [32]byte) error
}

// MemStore is the in-memory default implementation.
type MemStore struct {
	pool netip.Prefix

	mu          sync.RWMutex
	peers       map[string]*Peer     // key: base64(NodeKey)
	byTunnel    map[string]string    // tunnel_ip → base64(NodeKey)
	peersEtag   string
	peersBroad  chan struct{}

	signalMu    sync.Mutex
	signals     map[[32]byte][]Envelope
	signalWakes map[[32]byte]chan struct{}

	// maxQueueDepth is the cap on enqueued envelopes per recipient.
	maxQueueDepth int
	// maxAge drops envelopes older than this on pop.
	maxAge time.Duration
}

// NewMemStore constructs an empty in-memory store; pool is the CIDR from
// which tunnel IPs are allocated (must be IPv4).
func NewMemStore(pool netip.Prefix) *MemStore {
	return &MemStore{
		pool:          pool,
		peers:         make(map[string]*Peer),
		byTunnel:      make(map[string]string),
		peersEtag:     newEtag(),
		peersBroad:    make(chan struct{}),
		signals:       make(map[[32]byte][]Envelope),
		signalWakes:   make(map[[32]byte]chan struct{}),
		maxQueueDepth: 64,
		maxAge:        30 * time.Second,
	}
}

func newEtag() string {
	h := sha256.Sum256([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	return hex.EncodeToString(h[:8])
}

// Register inserts or updates a peer and assigns a tunnel IP. The mapping
// (nodePubkey → tunnel_ip) is stable across reconnects.
func (s *MemStore) Register(ctx context.Context, p Peer) (netip.Addr, error) {
	if err := ctx.Err(); err != nil {
		return netip.Addr{}, err
	}
	if len(p.NodeKey) != ed25519.PublicKeySize {
		return netip.Addr{}, fmt.Errorf("invalid node key length %d", len(p.NodeKey))
	}

	keyB64 := base64Encode(p.NodeKey)

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.peers[keyB64]; ok {
		existing.DiscoKey = p.DiscoKey
		existing.Name = p.Name
		existing.UpdatedAt = time.Now().UTC()
		s.bumpEtagLocked()
		return existing.TunnelIP, nil
	}

	ip, err := s.allocateIPLocked()
	if err != nil {
		return netip.Addr{}, err
	}

	p.TunnelIP = ip
	p.UpdatedAt = time.Now().UTC()
	peer := p
	s.peers[keyB64] = &peer
	s.byTunnel[ip.String()] = keyB64
	s.bumpEtagLocked()
	return ip, nil
}

func (s *MemStore) allocateIPLocked() (netip.Addr, error) {
	a := s.pool.Addr()
	// Skip network address (.0); assign sequentially.
	next := a.Next()
	for s.pool.Contains(next) {
		if _, taken := s.byTunnel[next.String()]; !taken {
			// Also skip what looks like a broadcast address: last address
			// in the prefix. Naive check: addr+1 must stay in prefix.
			if !s.pool.Contains(next.Next()) {
				break
			}
			return next, nil
		}
		next = next.Next()
	}
	return netip.Addr{}, errors.New("tunnel IP pool exhausted")
}

// SetEndpoints replaces the endpoints for a registered peer.
func (s *MemStore) SetEndpoints(ctx context.Context, nodeKey ed25519.PublicKey, eps []Endpoint) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	keyB64 := base64Encode(nodeKey)

	s.mu.Lock()
	defer s.mu.Unlock()

	peer, ok := s.peers[keyB64]
	if !ok {
		return errors.New("unknown peer")
	}
	peer.Endpoints = append(peer.Endpoints[:0], eps...)
	peer.UpdatedAt = time.Now().UTC()
	s.bumpEtagLocked()
	return nil
}

// Peers returns all registered peers and the current etag.
func (s *MemStore) Peers(ctx context.Context) ([]Peer, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Peer, 0, len(s.peers))
	for _, p := range s.peers {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].TunnelIP.Less(out[j].TunnelIP)
	})
	return out, s.peersEtag, nil
}

// WaitForPeersChange blocks until the etag differs from `since`, or ctx
// fires. Returns nil on change, ctx.Err() on cancellation.
func (s *MemStore) WaitForPeersChange(ctx context.Context, since string) error {
	s.mu.RLock()
	if s.peersEtag != since {
		s.mu.RUnlock()
		return nil
	}
	ch := s.peersBroad
	s.mu.RUnlock()

	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *MemStore) bumpEtagLocked() {
	s.peersEtag = newEtag()
	old := s.peersBroad
	s.peersBroad = make(chan struct{})
	close(old)
}

// EnqueueSignal adds a sealed envelope to the recipient's queue.
func (s *MemStore) EnqueueSignal(ctx context.Context, to [32]byte, env Envelope) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.signalMu.Lock()
	q := s.signals[to]
	if len(q) >= s.maxQueueDepth {
		q = q[1:]
	}
	q = append(q, env)
	s.signals[to] = q
	wake := s.signalWakes[to]
	delete(s.signalWakes, to)
	s.signalMu.Unlock()

	if wake != nil {
		close(wake)
	}
	return nil
}

// PopSignals drains and returns all queued envelopes for the recipient that
// are newer than s.maxAge.
func (s *MemStore) PopSignals(ctx context.Context, to [32]byte) ([]Envelope, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.signalMu.Lock()
	defer s.signalMu.Unlock()
	queue := s.signals[to]
	delete(s.signals, to)

	cutoff := time.Now().Add(-s.maxAge)
	fresh := queue[:0]
	for _, e := range queue {
		if e.Enqueue.After(cutoff) {
			fresh = append(fresh, e)
		}
	}
	return fresh, nil
}

// WaitForSignal blocks until an envelope is enqueued for `to`, or ctx fires.
func (s *MemStore) WaitForSignal(ctx context.Context, to [32]byte) error {
	s.signalMu.Lock()
	if len(s.signals[to]) > 0 {
		s.signalMu.Unlock()
		return nil
	}
	ch, ok := s.signalWakes[to]
	if !ok {
		ch = make(chan struct{})
		s.signalWakes[to] = ch
	}
	s.signalMu.Unlock()

	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// nodeKeyKey returns a stable string key for the peer map. Hex is used rather
// than base64 to avoid ambiguity around URL-safe vs standard encoding.
func base64Encode(b []byte) string {
	return hex.EncodeToString(b)
}
