package disco

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSha256Sum(t *testing.T) {
	got := sha256Sum([]byte("hello"))
	want := sha256.Sum256([]byte("hello"))
	if !bytes.Equal(got, want[:]) {
		t.Errorf("sha256Sum mismatch")
	}
	// Empty input has a well-known digest.
	empty := sha256Sum(nil)
	if len(empty) != 32 {
		t.Errorf("digest should be 32 bytes, got %d", len(empty))
	}
}

func TestNormalisePath(t *testing.T) {
	cases := map[string]string{
		"/v1/peers":                  "/v1/peers",
		"/v1/peers?since=abc":        "/v1/peers",
		"/v1/peers?":                 "/v1/peers",
		"":                           "",
		"?only-query":                "",
		"/v1/peers?a=1&b=2":          "/v1/peers",
	}
	for in, want := range cases {
		if got := normalisePath(in); got != want {
			t.Errorf("normalisePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIndexQuery(t *testing.T) {
	cases := map[string]int{
		"":              -1,
		"/no/query":     -1,
		"?":             0,
		"/p?q":          2,
		"/p/q/r?x=1":    6,
	}
	for in, want := range cases {
		if got := indexQuery(in); got != want {
			t.Errorf("indexQuery(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestSigningMaterial_Deterministic(t *testing.T) {
	body := []byte(`{"foo":"bar"}`)
	a := signingMaterial("100", "POST", "/v1/x", body)
	b := signingMaterial("100", "POST", "/v1/x", body)
	if !bytes.Equal(a, b) {
		t.Errorf("signingMaterial should be deterministic")
	}

	// Different timestamp → different digest.
	c := signingMaterial("101", "POST", "/v1/x", body)
	if bytes.Equal(a, c) {
		t.Errorf("different timestamps should produce different digests")
	}

	// Different body → different digest.
	d := signingMaterial("100", "POST", "/v1/x", []byte(`{"foo":"baz"}`))
	if bytes.Equal(a, d) {
		t.Errorf("different bodies should produce different digests")
	}
}

func TestSigningMaterial_Layout(t *testing.T) {
	// Layout: ts \n method \n path \n sha256(body)
	out := signingMaterial("42", "GET", "/x", []byte("body"))
	parts := bytes.SplitN(out, []byte("\n"), 4)
	if len(parts) != 4 {
		t.Fatalf("expected 4 newline-separated fields, got %d", len(parts))
	}
	if string(parts[0]) != "42" || string(parts[1]) != "GET" || string(parts[2]) != "/x" {
		t.Errorf("bad header fields: %q %q %q", parts[0], parts[1], parts[2])
	}
	want := sha256.Sum256([]byte("body"))
	if !bytes.Equal(parts[3], want[:]) {
		t.Errorf("body digest wrong")
	}
}

func TestDecodeDiscoPub(t *testing.T) {
	var raw [32]byte
	for i := range raw {
		raw[i] = byte(i)
	}
	s := base64.StdEncoding.EncodeToString(raw[:])
	got, err := DecodeDiscoPub(s)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != raw {
		t.Errorf("round-trip mismatch")
	}

	if _, err := DecodeDiscoPub("not-base64!!"); err == nil {
		t.Error("invalid b64 should error")
	}

	// Valid b64 but wrong length.
	short := base64.StdEncoding.EncodeToString([]byte{1, 2, 3})
	if _, err := DecodeDiscoPub(short); err == nil {
		t.Error("short pubkey should error")
	}
}

func TestB64(t *testing.T) {
	nk, err := GenerateNodeKey()
	if err != nil {
		t.Fatal(err)
	}
	dk, err := GenerateDiscoKey()
	if err != nil {
		t.Fatal(err)
	}

	nb := nk.B64()
	if _, err := base64.StdEncoding.DecodeString(nb); err != nil {
		t.Errorf("NodeKey.B64 not decodable: %v", err)
	}
	db := dk.B64()
	decoded, err := base64.StdEncoding.DecodeString(db)
	if err != nil {
		t.Fatalf("DiscoKey.B64 not decodable: %v", err)
	}
	if !bytes.Equal(decoded, dk.Pub[:]) {
		t.Error("DiscoKey.B64 roundtrip mismatch")
	}
}

func TestSealOpen_WrongRecipient(t *testing.T) {
	alice, _ := GenerateDiscoKey()
	bob, _ := GenerateDiscoKey()
	carol, _ := GenerateDiscoKey()

	sealed, err := Seal([]byte("hi"), bob.Pub, alice)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := Open(sealed, alice.Pub, carol); ok {
		t.Error("decrypt should fail with wrong recipient key")
	}
}

func TestSealOpen_ShortCiphertext(t *testing.T) {
	bob, err := GenerateDiscoKey()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := Open([]byte("short"), bob.Pub, bob); ok {
		t.Error("open should reject sub-nonce-length input")
	}
}

func TestParseEnvelope_TooShort(t *testing.T) {
	if _, _, err := ParseEnvelope([]byte("short")); err == nil {
		t.Error("expected too-short error")
	}
}

func TestBody_MarshalUnmarshal(t *testing.T) {
	in := Body{Type: MsgCallMeMaybe, Endpoints: []string{"1.2.3.4:5555", "6.7.8.9:10"}}
	raw, err := in.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	out, err := UnmarshalBody(raw)
	if err != nil {
		t.Fatal(err)
	}
	if out.Type != in.Type || len(out.Endpoints) != len(in.Endpoints) {
		t.Errorf("roundtrip lost fields: %+v vs %+v", out, in)
	}

	// Empty/invalid JSON.
	if _, err := UnmarshalBody([]byte("not json")); err == nil {
		t.Error("should error on bad JSON")
	}
}

func TestBody_OmitEmptyFields(t *testing.T) {
	// ping body omits src & endpoints; pong omits endpoints & node_key.
	ping := Body{Type: MsgPing, Tx: "abc", NodeKey: "nk"}
	raw, err := ping.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(`"endpoints"`)) || bytes.Contains(raw, []byte(`"src"`)) {
		t.Errorf("ping should omit endpoints/src: %s", raw)
	}
}

func TestLoadOrCreateKeys_BadJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "keys.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadOrCreateKeys(dir); err == nil {
		t.Error("expected JSON decode error")
	}
}

func TestLoadOrCreateKeys_BadB64(t *testing.T) {
	dir := t.TempDir()
	bad := []byte(`{"node_priv":"!!!","node_pub":"x","disco_priv":"y","disco_pub":"z"}`)
	if err := os.WriteFile(filepath.Join(dir, "keys.json"), bad, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadOrCreateKeys(dir); err == nil {
		t.Error("expected base64 decode error")
	}
}

func TestLoadOrCreateKeys_WrongSize(t *testing.T) {
	dir := t.TempDir()
	short := base64.StdEncoding.EncodeToString([]byte{1, 2, 3})
	p := map[string]string{
		"node_priv":  short,
		"node_pub":   short,
		"disco_priv": short,
		"disco_pub":  short,
	}
	raw, _ := json.Marshal(p)
	if err := os.WriteFile(filepath.Join(dir, "keys.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadOrCreateKeys(dir); err == nil {
		t.Error("expected size mismatch error")
	}
}

func TestLoadOrCreateKeys_FileMode(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := LoadOrCreateKeys(dir); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "keys.json"))
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("keys.json mode = %o, want 0600", mode)
	}
}

// ------------- CoordClient HTTP tests -------------

func TestCoordClient_Register(t *testing.T) {
	nk, _ := GenerateNodeKey()
	dk, _ := GenerateDiscoKey()

	var got struct {
		NodeName string `json:"node_name"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/register" || r.Method != "POST" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"tunnel_ip":"100.64.0.5/24","peers_etag":"etag1"}`))
	}))
	defer srv.Close()

	c := NewCoordClient(srv.URL, nk, dk)
	ip, etag, err := c.Register(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if ip != "100.64.0.5/24" || etag != "etag1" {
		t.Errorf("unexpected response: %q %q", ip, etag)
	}
	if got.NodeName != "alice" {
		t.Errorf("node name not marshalled: %q", got.NodeName)
	}
}

func TestCoordClient_Register_HTTPError(t *testing.T) {
	nk, _ := GenerateNodeKey()
	dk, _ := GenerateDiscoKey()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "name taken", http.StatusConflict)
	}))
	defer srv.Close()
	c := NewCoordClient(srv.URL, nk, dk)
	if _, _, err := c.Register(context.Background(), "x"); err == nil {
		t.Error("expected error on 409")
	}
}

func TestCoordClient_PostEndpoints_SignsRequest(t *testing.T) {
	nk, _ := GenerateNodeKey()
	dk, _ := GenerateDiscoKey()

	var sigOK atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify signature header shape (full validation lives in coord tests).
		ts := r.Header.Get("X-Gretun-Timestamp")
		node := r.Header.Get("X-Gretun-Node")
		auth := r.Header.Get("Authorization")
		if ts == "" || node == "" || !strings.HasPrefix(auth, "Gretun ") {
			http.Error(w, "unsigned", http.StatusUnauthorized)
			return
		}
		// Verify the signature itself round-trips with the node pubkey.
		body, _ := io.ReadAll(r.Body)
		pub, err := base64.StdEncoding.DecodeString(node)
		if err != nil {
			http.Error(w, "bad node header", http.StatusBadRequest)
			return
		}
		sig, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(auth, "Gretun "))
		if err != nil {
			http.Error(w, "bad auth header", http.StatusBadRequest)
			return
		}
		digest := signingMaterial(ts, r.Method, r.URL.Path, body)
		if ed25519.Verify(pub, digest, sig) {
			sigOK.Store(true)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := NewCoordClient(srv.URL, nk, dk)
	ap, _ := netip.ParseAddrPort("1.2.3.4:5555")
	if err := c.PostEndpoints(context.Background(), []RemoteEndpoint{
		{Addr: ap, Source: "stun"},
	}); err != nil {
		t.Fatal(err)
	}
	if !sigOK.Load() {
		t.Error("signature did not verify on server side")
	}
}

func TestCoordClient_Peers_RoundTrip(t *testing.T) {
	nk, _ := GenerateNodeKey()
	dk, _ := GenerateDiscoKey()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("since") != "abc" {
			http.Error(w, "no since", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{
			"etag":"next",
			"peers":[{
				"node_pubkey":"AQID",
				"disco_pubkey":[0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],
				"node_name":"bob",
				"tunnel_ip":"100.64.0.2",
				"endpoints":[{"addr":"1.2.3.4:5555","source":"stun"}],
				"updated_at":"2024-01-01T00:00:00Z"
			}]
		}`))
	}))
	defer srv.Close()

	c := NewCoordClient(srv.URL, nk, dk)
	peers, etag, err := c.Peers(context.Background(), "abc")
	if err != nil {
		t.Fatal(err)
	}
	if etag != "next" {
		t.Errorf("etag = %q, want next", etag)
	}
	if len(peers) != 1 {
		t.Fatalf("want 1 peer, got %d", len(peers))
	}
	if peers[0].Name != "bob" {
		t.Errorf("name wrong: %q", peers[0].Name)
	}
	if len(peers[0].Endpoints) != 1 || peers[0].Endpoints[0].Source != "stun" {
		t.Errorf("endpoint not converted: %+v", peers[0].Endpoints)
	}
}

func TestCoordClient_Peers_HTTPError(t *testing.T) {
	nk, _ := GenerateNodeKey()
	dk, _ := GenerateDiscoKey()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := NewCoordClient(srv.URL, nk, dk)
	if _, _, err := c.Peers(context.Background(), ""); err == nil {
		t.Error("expected error on 500")
	}
}

func TestCoordClient_SendAndPullSignals(t *testing.T) {
	nk, _ := GenerateNodeKey()
	dk, _ := GenerateDiscoKey()

	var receivedTo [32]byte
	var receivedSealed []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/signal":
			if r.Method == "POST" {
				var req struct {
					To     [32]byte `json:"to"`
					Sealed []byte   `json:"sealed"`
				}
				_ = json.NewDecoder(r.Body).Decode(&req)
				receivedTo = req.To
				receivedSealed = req.Sealed
				_, _ = w.Write([]byte(`{}`))
			} else {
				_, _ = w.Write([]byte(`{"envelopes":[{"sealed":"YWJjZA==","enqueue":"2024-01-01T00:00:00Z"}]}`))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewCoordClient(srv.URL, nk, dk)
	var to [32]byte
	to[0] = 7
	if err := c.SendSignal(context.Background(), to, []byte("sealed")); err != nil {
		t.Fatal(err)
	}
	if receivedTo != to {
		t.Error("recipient not round-tripped")
	}
	if !bytes.Equal(receivedSealed, []byte("sealed")) {
		t.Errorf("sealed body lost: %q", receivedSealed)
	}

	msgs, err := c.PullSignals(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || string(msgs[0]) != "abcd" {
		t.Errorf("unexpected pull result: %+v", msgs)
	}
}

func TestCoordClient_NetworkError(t *testing.T) {
	nk, _ := GenerateNodeKey()
	dk, _ := GenerateDiscoKey()
	// Close the server immediately so the client gets a connection error.
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()

	c := NewCoordClient(srv.URL, nk, dk)
	if _, _, err := c.Register(context.Background(), "x"); err == nil {
		t.Error("expected network error")
	}
}

func TestCoordClient_ContextCancelled(t *testing.T) {
	nk, _ := GenerateNodeKey()
	dk, _ := GenerateDiscoKey()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// never reached because context is already cancelled
	}))
	defer srv.Close()

	c := NewCoordClient(srv.URL, nk, dk)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := c.Register(ctx, "x"); !errors.Is(err, context.Canceled) {
		// Connection attempt may fail with a wrapped error — the key is we don't hang.
		if err == nil {
			t.Error("expected error with cancelled context")
		}
	}
}

