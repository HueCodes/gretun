package coord

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"testing"
	"time"

	"github.com/HueCodes/gretun/internal/disco"
)

type testClient struct {
	base string
	nk   disco.NodeKey
	dk   disco.DiscoKey
	http *http.Client
}

func newTestClient(t *testing.T, base string) *testClient {
	t.Helper()
	nk, err := disco.GenerateNodeKey()
	if err != nil {
		t.Fatal(err)
	}
	dk, err := disco.GenerateDiscoKey()
	if err != nil {
		t.Fatal(err)
	}
	return &testClient{base: base, nk: nk, dk: dk, http: &http.Client{Timeout: 5 * time.Second}}
}

func (c *testClient) do(t *testing.T, method, path string, body any) *http.Response {
	t.Helper()
	var buf []byte
	if body != nil {
		var err error
		buf, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req, _ := http.NewRequest(method, c.base+path, bytes.NewReader(buf))
	if path != "/v1/register" {
		ts, nodeB64, auth := SignRequest(c.nk.Priv, c.nk.Pub, method, path, buf)
		req.Header.Set("X-Gretun-Timestamp", ts)
		req.Header.Set("X-Gretun-Node", nodeB64)
		req.Header.Set("Authorization", auth)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func (c *testClient) register(t *testing.T) RegisterResp {
	t.Helper()
	resp := c.do(t, "POST", "/v1/register", RegisterReq{
		NodePubkey:  c.nk.Pub,
		DiscoPubkey: c.dk.Pub,
		NodeName:    "test",
	})
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("register status=%d body=%s", resp.StatusCode, string(b))
	}
	var out RegisterResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestServer_RegisterAndPeers(t *testing.T) {
	store := NewMemStore(netip.MustParsePrefix("100.64.0.0/24"))
	srv := httptest.NewServer(NewServer(store))
	defer srv.Close()

	a := newTestClient(t, srv.URL)
	ra := a.register(t)
	if ra.TunnelIP == "" {
		t.Fatal("no tunnel IP assigned")
	}
	b := newTestClient(t, srv.URL)
	b.register(t)

	resp := a.do(t, "GET", "/v1/peers", nil)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("peers status=%d", resp.StatusCode)
	}
	var pr PeersResp
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		t.Fatal(err)
	}
	if len(pr.Peers) != 2 {
		t.Errorf("want 2 peers, got %d", len(pr.Peers))
	}
}

func TestServer_SignalRelay(t *testing.T) {
	store := NewMemStore(netip.MustParsePrefix("100.64.0.0/24"))
	srv := httptest.NewServer(NewServer(store))
	defer srv.Close()

	a := newTestClient(t, srv.URL)
	a.register(t)
	b := newTestClient(t, srv.URL)
	b.register(t)

	// A sends a sealed envelope to B.
	envBytes, err := disco.BuildEnvelope(a.dk, b.dk.Pub, disco.Body{Type: disco.MsgPing, Tx: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	resp := a.do(t, "POST", "/v1/signal", SignalReq{To: b.dk.Pub, Sealed: envBytes})
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("send status=%d", resp.StatusCode)
	}

	// B pulls.
	resp = b.do(t, "GET", "/v1/signal", nil)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("pull status=%d", resp.StatusCode)
	}
	var sr SignalsResp
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		t.Fatal(err)
	}
	if len(sr.Envelopes) != 1 {
		t.Fatalf("want 1 envelope, got %d", len(sr.Envelopes))
	}
	sender, body, err := disco.OpenEnvelope(sr.Envelopes[0].Sealed, b.dk)
	if err != nil {
		t.Fatal(err)
	}
	if sender != a.dk.Pub {
		t.Error("sender mismatch")
	}
	if body.Tx != "hello" || body.Type != disco.MsgPing {
		t.Errorf("body mismatch: %+v", body)
	}
}

func TestServer_RejectsBadSignature(t *testing.T) {
	store := NewMemStore(netip.MustParsePrefix("100.64.0.0/24"))
	srv := httptest.NewServer(NewServer(store))
	defer srv.Close()

	a := newTestClient(t, srv.URL)
	a.register(t)

	// Forge a request: sign with a different key but claim to be `a`.
	forged, _ := disco.GenerateNodeKey()
	buf, _ := json.Marshal(EndpointsReq{Endpoints: []Endpoint{{Addr: netip.MustParseAddrPort("1.2.3.4:5"), Source: SourceSTUN}}})
	req, _ := http.NewRequest("POST", srv.URL+"/v1/endpoints", bytes.NewReader(buf))
	ts, _, auth := SignRequest(forged.Priv, forged.Pub, "POST", "/v1/endpoints", buf)
	req.Header.Set("X-Gretun-Timestamp", ts)
	// Claim to be `a` by using a's pub key but the signature is by `forged`.
	req.Header.Set("X-Gretun-Node", a.nk.B64())
	req.Header.Set("Authorization", auth)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", resp.StatusCode)
	}
}

func TestServer_RejectsStaleTimestamp(t *testing.T) {
	store := NewMemStore(netip.MustParsePrefix("100.64.0.0/24"))
	srv := httptest.NewServer(NewServer(store))
	defer srv.Close()

	a := newTestClient(t, srv.URL)
	a.register(t)

	buf := []byte{}
	req, _ := http.NewRequest("GET", srv.URL+"/v1/peers", nil)
	// Stamp the request 10 minutes in the past.
	stale := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)
	digest := signingMaterial(stale, "GET", "/v1/peers", buf)
	sig := ed25519.Sign(a.nk.Priv, digest)
	req.Header.Set("X-Gretun-Timestamp", stale)
	req.Header.Set("X-Gretun-Node", a.nk.B64())
	req.Header.Set("Authorization", authScheme+" "+base64.StdEncoding.EncodeToString(sig))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", resp.StatusCode)
	}
}

func TestServer_PeersLongPollReturnsOnChange(t *testing.T) {
	store := NewMemStore(netip.MustParsePrefix("100.64.0.0/24"))
	srv := httptest.NewServer(NewServer(store))
	defer srv.Close()

	a := newTestClient(t, srv.URL)
	a.register(t)
	_, firstEtag, _ := store.Peers(context.Background())

	done := make(chan time.Duration, 1)
	go func() {
		start := time.Now()
		resp := a.do(t, "GET", "/v1/peers?since="+firstEtag, nil)
		resp.Body.Close()
		done <- time.Since(start)
	}()

	// Give the poller a moment to block, then register a second peer.
	time.Sleep(100 * time.Millisecond)
	b := newTestClient(t, srv.URL)
	b.register(t)

	select {
	case d := <-done:
		if d > longPollTimeout-time.Second {
			t.Errorf("long-poll returned in %v — did not wake on change", d)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("long-poll never returned")
	}
}

