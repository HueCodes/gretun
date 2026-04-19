package coord

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	authScheme   = "Gretun"
	headerAuth   = "Authorization"
	headerTs     = "X-Gretun-Timestamp"
	headerKey    = "X-Gretun-Node"
	maxClockSkew = 60 * time.Second
)

// SignRequest computes the Authorization header value for a request body.
// Signed material: timestamp || "\n" || method || "\n" || path || "\n" || sha256(body).
// Returned values should be set as X-Gretun-Timestamp, X-Gretun-Node, and
// Authorization on the outgoing HTTP request.
func SignRequest(priv ed25519.PrivateKey, pub ed25519.PublicKey, method, path string, body []byte) (ts, nodeB64, authHeader string) {
	ts = strconv.FormatInt(time.Now().UTC().Unix(), 10)
	digest := signingMaterial(ts, method, path, body)
	sig := ed25519.Sign(priv, digest)
	nodeB64 = base64.StdEncoding.EncodeToString(pub)
	authHeader = authScheme + " " + base64.StdEncoding.EncodeToString(sig)
	return
}

// VerifyRequest validates the signature on an incoming HTTP request.
// On success, it returns the requester's Ed25519 public key and the already-
// consumed request body (readers can't be re-read after verification).
func VerifyRequest(r *http.Request) (ed25519.PublicKey, []byte, error) {
	ts := r.Header.Get(headerTs)
	nodeB64 := r.Header.Get(headerKey)
	auth := r.Header.Get(headerAuth)
	if ts == "" || nodeB64 == "" || auth == "" {
		return nil, nil, errors.New("missing auth headers")
	}
	if !strings.HasPrefix(auth, authScheme+" ") {
		return nil, nil, errors.New("bad auth scheme")
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(auth, authScheme+" "))
	if err != nil {
		return nil, nil, fmt.Errorf("decode sig: %w", err)
	}
	pubBytes, err := base64.StdEncoding.DecodeString(nodeB64)
	if err != nil {
		return nil, nil, fmt.Errorf("decode node pub: %w", err)
	}
	if len(pubBytes) != ed25519.PublicKeySize {
		return nil, nil, fmt.Errorf("bad pubkey length %d", len(pubBytes))
	}

	tsInt, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return nil, nil, fmt.Errorf("bad timestamp: %w", err)
	}
	now := time.Now().UTC().Unix()
	if now-tsInt > int64(maxClockSkew.Seconds()) || tsInt-now > int64(maxClockSkew.Seconds()) {
		return nil, nil, errors.New("stale timestamp")
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("read body: %w", err)
	}
	_ = r.Body.Close()
	digest := signingMaterial(ts, r.Method, r.URL.Path, body)
	if !ed25519.Verify(pubBytes, digest, sig) {
		return nil, nil, errors.New("signature verify failed")
	}
	return ed25519.PublicKey(pubBytes), body, nil
}

func signingMaterial(ts, method, path string, body []byte) []byte {
	h := sha256.New()
	h.Write(body)
	bodyHash := h.Sum(nil)

	var buf bytes.Buffer
	buf.WriteString(ts)
	buf.WriteByte('\n')
	buf.WriteString(method)
	buf.WriteByte('\n')
	buf.WriteString(path)
	buf.WriteByte('\n')
	buf.Write(bodyHash)
	return buf.Bytes()
}
