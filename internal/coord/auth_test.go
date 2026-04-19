package coord

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// signedRequest returns a request that VerifyRequest would accept, then lets
// the caller tamper with headers before calling VerifyRequest.
func signedRequest(t *testing.T, body []byte) (*http.Request, ed25519.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/v1/endpoints", bytes.NewReader(body))
	ts, nodeB64, auth := SignRequest(priv, pub, "POST", "/v1/endpoints", body)
	req.Header.Set(headerTs, ts)
	req.Header.Set(headerKey, nodeB64)
	req.Header.Set(headerAuth, auth)
	return req, pub
}

func TestVerifyRequest_OK(t *testing.T) {
	body := []byte(`{"x":1}`)
	req, pub := signedRequest(t, body)
	gotPub, gotBody, err := VerifyRequest(req)
	if err != nil {
		t.Fatalf("VerifyRequest: %v", err)
	}
	if !bytes.Equal(gotPub, pub) {
		t.Error("returned pub does not match signer")
	}
	if !bytes.Equal(gotBody, body) {
		t.Errorf("body roundtrip mismatch: %s", gotBody)
	}
}

func TestVerifyRequest_MissingHeaders(t *testing.T) {
	for _, h := range []string{headerTs, headerKey, headerAuth} {
		t.Run(h, func(t *testing.T) {
			req, _ := signedRequest(t, []byte("b"))
			req.Header.Del(h)
			if _, _, err := VerifyRequest(req); err == nil {
				t.Errorf("expected error when %s is missing", h)
			}
		})
	}
}

func TestVerifyRequest_BadScheme(t *testing.T) {
	req, _ := signedRequest(t, []byte("b"))
	req.Header.Set(headerAuth, "Bearer xyz")
	if _, _, err := VerifyRequest(req); err == nil {
		t.Error("expected error for non-Gretun scheme")
	}
}

func TestVerifyRequest_BadSigBase64(t *testing.T) {
	req, _ := signedRequest(t, []byte("b"))
	req.Header.Set(headerAuth, authScheme+" @@@not-base64@@@")
	if _, _, err := VerifyRequest(req); err == nil {
		t.Error("expected base64 decode error")
	}
}

func TestVerifyRequest_BadNodeKeyBase64(t *testing.T) {
	req, _ := signedRequest(t, []byte("b"))
	req.Header.Set(headerKey, "!!!not-base64!!!")
	if _, _, err := VerifyRequest(req); err == nil {
		t.Error("expected base64 decode error")
	}
}

func TestVerifyRequest_BadPubkeyLength(t *testing.T) {
	req, _ := signedRequest(t, []byte("b"))
	short := base64.StdEncoding.EncodeToString([]byte{1, 2, 3})
	req.Header.Set(headerKey, short)
	if _, _, err := VerifyRequest(req); err == nil || !strings.Contains(err.Error(), "pubkey length") {
		t.Errorf("expected pubkey length error, got %v", err)
	}
}

func TestVerifyRequest_BadTimestampFormat(t *testing.T) {
	req, _ := signedRequest(t, []byte("b"))
	req.Header.Set(headerTs, "not-a-number")
	if _, _, err := VerifyRequest(req); err == nil {
		t.Error("expected timestamp parse error")
	}
}

func TestVerifyRequest_FutureTimestamp(t *testing.T) {
	req, _ := signedRequest(t, []byte("b"))
	// Stamp 5 minutes in the future — outside the 60s skew window.
	future := strconv.FormatInt(time.Now().Add(5*time.Minute).Unix(), 10)
	req.Header.Set(headerTs, future)
	if _, _, err := VerifyRequest(req); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Errorf("expected stale-timestamp error, got %v", err)
	}
}

func TestVerifyRequest_StaleTimestamp(t *testing.T) {
	req, _ := signedRequest(t, []byte("b"))
	stale := strconv.FormatInt(time.Now().Add(-5*time.Minute).Unix(), 10)
	req.Header.Set(headerTs, stale)
	if _, _, err := VerifyRequest(req); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Errorf("expected stale-timestamp error, got %v", err)
	}
}

func TestVerifyRequest_BodyTamper(t *testing.T) {
	// Sign against body A, replace with body B: signature must fail.
	pub, priv, _ := ed25519.GenerateKey(nil)
	orig := []byte(`{"x":1}`)
	ts, nodeB64, auth := SignRequest(priv, pub, "POST", "/v1/endpoints", orig)
	req := httptest.NewRequest("POST", "/v1/endpoints", bytes.NewReader([]byte(`{"x":2}`)))
	req.Header.Set(headerTs, ts)
	req.Header.Set(headerKey, nodeB64)
	req.Header.Set(headerAuth, auth)

	if _, _, err := VerifyRequest(req); err == nil {
		t.Error("body tamper should fail signature verification")
	}
}

func TestSigningMaterial_Shape(t *testing.T) {
	out := signingMaterial("123", "GET", "/x", []byte("body"))
	parts := bytes.SplitN(out, []byte("\n"), 4)
	if len(parts) != 4 {
		t.Fatalf("want 4 fields, got %d", len(parts))
	}
	if string(parts[0]) != "123" || string(parts[1]) != "GET" || string(parts[2]) != "/x" {
		t.Errorf("header fields wrong: %q %q %q", parts[0], parts[1], parts[2])
	}
	if len(parts[3]) != 32 {
		t.Errorf("body hash should be 32 bytes, got %d", len(parts[3]))
	}
}

