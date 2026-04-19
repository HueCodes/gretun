package coord

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/HueCodes/gretun/internal/disco"
)

func TestServer_Register_BadJSON(t *testing.T) {
	srv := httptest.NewServer(NewServer(newTestStore(t)))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/register", "application/json", strings.NewReader("{not json"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

func TestServer_Register_BadNodePubkeyLength(t *testing.T) {
	srv := httptest.NewServer(NewServer(newTestStore(t)))
	defer srv.Close()

	body, _ := json.Marshal(RegisterReq{NodePubkey: []byte{1, 2, 3}, NodeName: "x"})
	resp, err := http.Post(srv.URL+"/v1/register", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

func TestServer_Endpoints_BadJSON(t *testing.T) {
	srv := httptest.NewServer(NewServer(newTestStore(t)))
	defer srv.Close()

	a := newTestClient(t, srv.URL)
	a.register(t)

	// Manually craft a signed request with invalid JSON body.
	bad := []byte("not json")
	req, _ := http.NewRequest("POST", srv.URL+"/v1/endpoints", bytes.NewReader(bad))
	ts, nodeB64, auth := SignRequest(a.nk.Priv, a.nk.Pub, "POST", "/v1/endpoints", bad)
	req.Header.Set("X-Gretun-Timestamp", ts)
	req.Header.Set("X-Gretun-Node", nodeB64)
	req.Header.Set("Authorization", auth)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

func TestServer_Endpoints_UnregisteredPeer(t *testing.T) {
	srv := httptest.NewServer(NewServer(newTestStore(t)))
	defer srv.Close()

	// Client that never called /v1/register. The signed request still passes
	// VerifyRequest but the store returns "unknown peer".
	c := newTestClient(t, srv.URL)
	resp := c.do(t, "POST", "/v1/endpoints", EndpointsReq{Endpoints: []Endpoint{
		{Addr: netip.MustParseAddrPort("1.2.3.4:5"), Source: SourceSTUN},
	}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

func TestServer_Signal_BadJSON(t *testing.T) {
	srv := httptest.NewServer(NewServer(newTestStore(t)))
	defer srv.Close()

	a := newTestClient(t, srv.URL)
	a.register(t)

	bad := []byte("not json")
	req, _ := http.NewRequest("POST", srv.URL+"/v1/signal", bytes.NewReader(bad))
	ts, nodeB64, auth := SignRequest(a.nk.Priv, a.nk.Pub, "POST", "/v1/signal", bad)
	req.Header.Set("X-Gretun-Timestamp", ts)
	req.Header.Set("X-Gretun-Node", nodeB64)
	req.Header.Set("Authorization", auth)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

func TestServer_SignalPull_UnregisteredNode(t *testing.T) {
	srv := httptest.NewServer(NewServer(newTestStore(t)))
	defer srv.Close()

	// Never registered — pull should 400 since we can't map nodeKey → discoKey.
	c := newTestClient(t, srv.URL)
	resp := c.do(t, "GET", "/v1/signal", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

func TestServer_SignalPull_LongPollWakesOnSignal(t *testing.T) {
	store := newTestStore(t)
	srv := httptest.NewServer(NewServer(store))
	defer srv.Close()

	a := newTestClient(t, srv.URL)
	a.register(t)
	b := newTestClient(t, srv.URL)
	b.register(t)

	done := make(chan time.Duration, 1)
	go func() {
		start := time.Now()
		resp := b.do(t, "GET", "/v1/signal", nil)
		resp.Body.Close()
		done <- time.Since(start)
	}()

	time.Sleep(50 * time.Millisecond)
	envBytes, _ := disco.BuildEnvelope(a.dk, b.dk.Pub, disco.Body{Type: disco.MsgPing})
	resp := a.do(t, "POST", "/v1/signal", SignalReq{To: b.dk.Pub, Sealed: envBytes})
	resp.Body.Close()

	select {
	case d := <-done:
		if d > longPollTimeout-time.Second {
			t.Errorf("long-poll waited too long: %v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("long-poll never returned")
	}
}

func TestServer_DebugPeers(t *testing.T) {
	store := newTestStore(t)
	srv := httptest.NewServer(NewServer(store))
	defer srv.Close()

	a := newTestClient(t, srv.URL)
	a.register(t)

	// /debug/peers is unauthenticated.
	resp, err := http.Get(srv.URL + "/debug/peers")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	buf, _ := io.ReadAll(resp.Body)
	var pr PeersResp
	if err := json.Unmarshal(buf, &pr); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, buf)
	}
	if len(pr.Peers) != 1 {
		t.Errorf("want 1 peer, got %d", len(pr.Peers))
	}
}

func TestDiscoKeyForNodeKey_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := discoKeyForNodeKey(context.Background(), s, make([]byte, 32))
	if err == nil {
		t.Error("expected unregistered-node error")
	}
}

func TestDiscoKeyForNodeKey_Found(t *testing.T) {
	s := newTestStore(t)
	p := makePeer(t, "alice")
	if _, err := s.Register(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	dk, err := discoKeyForNodeKey(context.Background(), s, p.NodeKey)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if dk != p.DiscoKey {
		t.Errorf("disco key mismatch")
	}
}

func TestWriteJSON_MarshalError(t *testing.T) {
	// A channel value cannot be JSON-marshalled — writeJSON should 500.
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusOK, map[string]any{"bad": make(chan int)})
	if w.Code != http.StatusInternalServerError {
		t.Errorf("want 500, got %d", w.Code)
	}
}
