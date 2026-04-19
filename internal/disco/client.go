package disco

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strconv"
	"time"
)

// endpointForWire is the wire shape of an endpoint. Kept separate from the
// coord package to avoid a client → coord dependency.
type endpointForWire struct {
	Addr   netip.AddrPort `json:"addr"`
	Source string         `json:"source"`
}

// envelopeForWire is the wire shape of a relayed envelope.
type envelopeForWire struct {
	Sealed  []byte    `json:"sealed"`
	Enqueue time.Time `json:"enqueue"`
}

// CoordClient is a thin client for the coordinator HTTP API. It signs every
// non-register request with the caller's Ed25519 node key.
type CoordClient struct {
	base string
	nk   NodeKey
	dk   DiscoKey
	http *http.Client
}

// NewCoordClient constructs a client against base URL (e.g. http://coord:8443).
func NewCoordClient(base string, nk NodeKey, dk DiscoKey) *CoordClient {
	return &CoordClient{
		base: base,
		nk:   nk,
		dk:   dk,
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

// Register registers this node with the coordinator. The returned tunnel IP
// is in CIDR notation (e.g. "100.64.0.5/24").
func (c *CoordClient) Register(ctx context.Context, name string) (tunnelCIDR, etag string, err error) {
	body, _ := json.Marshal(struct {
		NodePubkey  []byte   `json:"node_pubkey"`
		DiscoPubkey [32]byte `json:"disco_pubkey"`
		NodeName    string   `json:"node_name"`
	}{NodePubkey: c.nk.Pub, DiscoPubkey: c.dk.Pub, NodeName: name})

	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/v1/register", bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("register: %d: %s", resp.StatusCode, string(b))
	}
	var out struct {
		TunnelIP string `json:"tunnel_ip"`
		Etag     string `json:"peers_etag"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", err
	}
	return out.TunnelIP, out.Etag, nil
}

// PostEndpoints publishes the node's current endpoint candidates.
func (c *CoordClient) PostEndpoints(ctx context.Context, eps []RemoteEndpoint) error {
	wire := make([]endpointForWire, len(eps))
	for i, e := range eps {
		wire[i] = endpointForWire{Addr: e.Addr, Source: string(e.Source)}
	}
	body, _ := json.Marshal(struct {
		Endpoints []endpointForWire `json:"endpoints"`
	}{Endpoints: wire})
	resp, err := c.signedDo(ctx, "POST", "/v1/endpoints", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("endpoints: %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

// RemoteEndpoint is the client-side peer endpoint tuple.
type RemoteEndpoint struct {
	Addr   netip.AddrPort
	Source string
}

// RemotePeer is what the daemon sees about another node.
type RemotePeer struct {
	NodeKey   ed25519.PublicKey
	DiscoKey  [32]byte
	Name      string
	TunnelIP  netip.Addr
	Endpoints []RemoteEndpoint
	UpdatedAt time.Time
}

// Peers fetches the peer list; if since != "" the coordinator long-polls.
func (c *CoordClient) Peers(ctx context.Context, since string) ([]RemotePeer, string, error) {
	path := "/v1/peers"
	if since != "" {
		path += "?since=" + since
	}
	resp, err := c.signedDo(ctx, "GET", path, nil)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, "", fmt.Errorf("peers: %d", resp.StatusCode)
	}

	var wire struct {
		Etag  string `json:"etag"`
		Peers []struct {
			NodeKey   []byte            `json:"node_pubkey"`
			DiscoKey  [32]byte          `json:"disco_pubkey"`
			Name      string            `json:"node_name"`
			TunnelIP  netip.Addr        `json:"tunnel_ip"`
			Endpoints []endpointForWire `json:"endpoints"`
			UpdatedAt time.Time         `json:"updated_at"`
		} `json:"peers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return nil, "", err
	}
	out := make([]RemotePeer, 0, len(wire.Peers))
	for _, p := range wire.Peers {
		eps := make([]RemoteEndpoint, 0, len(p.Endpoints))
		for _, e := range p.Endpoints {
			eps = append(eps, RemoteEndpoint(e))
		}
		out = append(out, RemotePeer{
			NodeKey: p.NodeKey, DiscoKey: p.DiscoKey, Name: p.Name,
			TunnelIP: p.TunnelIP, Endpoints: eps, UpdatedAt: p.UpdatedAt,
		})
	}
	return out, wire.Etag, nil
}

// SendSignal enqueues a sealed envelope for delivery to recipient.
func (c *CoordClient) SendSignal(ctx context.Context, to [32]byte, sealed []byte) error {
	body, _ := json.Marshal(struct {
		To     [32]byte `json:"to"`
		Sealed []byte   `json:"sealed"`
	}{To: to, Sealed: sealed})
	resp, err := c.signedDo(ctx, "POST", "/v1/signal", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("signal: %d", resp.StatusCode)
	}
	return nil
}

// PullSignals long-polls for envelopes addressed to this node's disco key.
func (c *CoordClient) PullSignals(ctx context.Context) ([][]byte, error) {
	resp, err := c.signedDo(ctx, "GET", "/v1/signal", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("signal pull: %d", resp.StatusCode)
	}
	var out struct {
		Envelopes []envelopeForWire `json:"envelopes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	sealed := make([][]byte, 0, len(out.Envelopes))
	for _, e := range out.Envelopes {
		sealed = append(sealed, e.Sealed)
	}
	return sealed, nil
}

// signedDo issues a request with the gretun signature headers attached.
func (c *CoordClient) signedDo(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	ts := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	digest := signingMaterial(ts, method, normalisePath(path), body)
	sig := ed25519.Sign(c.nk.Priv, digest)
	req.Header.Set("X-Gretun-Timestamp", ts)
	req.Header.Set("X-Gretun-Node", base64.StdEncoding.EncodeToString(c.nk.Pub))
	req.Header.Set("Authorization", "Gretun "+base64.StdEncoding.EncodeToString(sig))
	return c.http.Do(req)
}

// normalisePath strips the query string from a request path so that the
// signature covers the route only, not the query. This matches the server
// which uses r.URL.Path for the signing material.
func normalisePath(p string) string {
	if i := indexQuery(p); i >= 0 {
		return p[:i]
	}
	return p
}

func indexQuery(p string) int {
	for i, c := range p {
		if c == '?' {
			return i
		}
	}
	return -1
}

func signingMaterial(ts, method, path string, body []byte) []byte {
	var b bytes.Buffer
	b.WriteString(ts)
	b.WriteByte('\n')
	b.WriteString(method)
	b.WriteByte('\n')
	b.WriteString(path)
	b.WriteByte('\n')
	h := sha256Sum(body)
	b.Write(h)
	return b.Bytes()
}

// ErrNoPeer is returned when a signalling target isn't registered yet.
var ErrNoPeer = errors.New("no such peer")
