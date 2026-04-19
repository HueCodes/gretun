// Package coord implements the gretun coordinator: a dumb peer registry
// plus a blind signaling relay. The coordinator never sees tunnel payload
// and never has a disco private key, so a compromise only reveals peer
// public metadata (node pubkeys, public endpoints) — not tunnel contents.
package coord

import (
	"net/netip"
	"time"
)

// EndpointSource tags where an endpoint was learned. "local" means a NIC
// address the peer saw on itself; "stun" means a public mapping reported
// by a STUN server. Punching prefers stun but tries all.
type EndpointSource string

const (
	SourceLocal EndpointSource = "local"
	SourceSTUN  EndpointSource = "stun"
)

// Endpoint is one ip:port candidate for reaching a peer.
type Endpoint struct {
	Addr   netip.AddrPort `json:"addr"`
	Source EndpointSource `json:"source"`
}

// Peer is what the registry knows about a registered node.
type Peer struct {
	NodeKey     []byte         `json:"node_pubkey"` // Ed25519 pubkey (32 bytes)
	DiscoKey    [32]byte       `json:"disco_pubkey"`
	Name        string         `json:"node_name"`
	TunnelIP    netip.Addr     `json:"tunnel_ip"`
	Endpoints   []Endpoint     `json:"endpoints"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// Envelope is the opaque relay payload. The coordinator never peeks inside
// Sealed; it just forwards (To, sealed bytes) to the addressed peer.
type Envelope struct {
	From    [32]byte  `json:"from"`  // sender disco pubkey
	Sealed  []byte    `json:"sealed"` // raw envelope bytes (magic+sender+sealed body)
	Enqueue time.Time `json:"enqueue"`
}

// RegisterReq is the body of POST /v1/register.
type RegisterReq struct {
	NodePubkey        []byte   `json:"node_pubkey"`
	DiscoPubkey       [32]byte `json:"disco_pubkey"`
	NodeName          string   `json:"node_name"`
	RequestedTunnelIP string   `json:"requested_tunnel_ip,omitempty"`
}

// RegisterResp is the response for POST /v1/register.
type RegisterResp struct {
	TunnelIP string `json:"tunnel_ip"`
	Etag     string `json:"peers_etag"`
}

// EndpointsReq is the body of POST /v1/endpoints.
type EndpointsReq struct {
	Endpoints []Endpoint `json:"endpoints"`
}

// PeersResp is the body returned by GET /v1/peers.
type PeersResp struct {
	Etag  string `json:"etag"`
	Peers []Peer `json:"peers"`
}

// SignalReq is the body of POST /v1/signal.
type SignalReq struct {
	To     [32]byte `json:"to"`
	Sealed []byte   `json:"sealed"`
}

// SignalsResp is the body returned by GET /v1/signal.
type SignalsResp struct {
	Envelopes []Envelope `json:"envelopes"`
}
