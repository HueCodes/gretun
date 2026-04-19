package coord

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// longPollTimeout is how long a /peers or /signal long-poll will hold a
// connection open waiting for something to change.
const longPollTimeout = 25 * time.Second

// Server serves the coordinator HTTP API against a Store.
type Server struct {
	store Store
	mux   *http.ServeMux
	log   *slog.Logger
}

// NewServer wires up the HTTP handlers. register is unauthenticated (the
// requester is by definition not yet in the store); everything else checks
// an Ed25519 signature over the request.
func NewServer(store Store) *Server {
	s := &Server{
		store: store,
		mux:   http.NewServeMux(),
		log:   slog.Default(),
	}
	s.mux.HandleFunc("POST /v1/register", s.handleRegister)
	s.mux.HandleFunc("POST /v1/endpoints", s.authed(s.handleEndpoints))
	s.mux.HandleFunc("GET /v1/peers", s.authed(s.handlePeers))
	s.mux.HandleFunc("POST /v1/signal", s.authed(s.handleSignal))
	s.mux.HandleFunc("GET /v1/signal", s.authed(s.handleSignalPull))
	s.mux.HandleFunc("GET /debug/peers", s.handleDebugPeers)
	return s
}

// ServeHTTP makes Server an http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

// handlerFunc is the shape of a handler that already has the authed peer.
type handlerFunc func(w http.ResponseWriter, r *http.Request, pub ed25519.PublicKey, body []byte)

func (s *Server) authed(h handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pub, body, err := VerifyRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		h(w, r, pub, body)
	}
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req RegisterReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if len(req.NodePubkey) != ed25519.PublicKeySize {
		http.Error(w, "bad node_pubkey length", http.StatusBadRequest)
		return
	}
	tunnelIP, err := s.store.Register(r.Context(), Peer{
		NodeKey:  req.NodePubkey,
		DiscoKey: req.DiscoPubkey,
		Name:     req.NodeName,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, etag, _ := s.store.Peers(r.Context())
	writeJSON(w, http.StatusOK, RegisterResp{TunnelIP: tunnelIP.String() + "/24", Etag: etag})
}

func (s *Server) handleEndpoints(w http.ResponseWriter, r *http.Request, pub ed25519.PublicKey, body []byte) {
	var req EndpointsReq
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if err := s.store.SetEndpoints(r.Context(), pub, req.Endpoints); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handlePeers(w http.ResponseWriter, r *http.Request, _ ed25519.PublicKey, _ []byte) {
	since := r.URL.Query().Get("since")
	if since != "" {
		ctx, cancel := context.WithTimeout(r.Context(), longPollTimeout)
		defer cancel()
		_ = s.store.WaitForPeersChange(ctx, since)
	}
	peers, etag, err := s.store.Peers(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, PeersResp{Etag: etag, Peers: peers})
}

func (s *Server) handleSignal(w http.ResponseWriter, r *http.Request, pub ed25519.PublicKey, body []byte) {
	var req SignalReq
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	env := Envelope{Sealed: req.Sealed, Enqueue: time.Now()}
	if err := s.store.EnqueueSignal(r.Context(), req.To, env); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"queued": true})
}

func (s *Server) handleSignalPull(w http.ResponseWriter, r *http.Request, pub ed25519.PublicKey, _ []byte) {
	to, err := discoKeyForNodeKey(r.Context(), s.store, pub)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	envs, _ := s.store.PopSignals(r.Context(), to)
	if len(envs) == 0 {
		ctx, cancel := context.WithTimeout(r.Context(), longPollTimeout)
		defer cancel()
		if werr := s.store.WaitForSignal(ctx, to); werr == nil {
			envs, _ = s.store.PopSignals(r.Context(), to)
		}
	}
	writeJSON(w, http.StatusOK, SignalsResp{Envelopes: envs})
}

func (s *Server) handleDebugPeers(w http.ResponseWriter, r *http.Request) {
	peers, etag, err := s.store.Peers(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, PeersResp{Etag: etag, Peers: peers})
}

// discoKeyForNodeKey looks up the caller's disco pubkey — we need it to find
// the caller's signal queue, but the HTTP auth uses the node pubkey.
func discoKeyForNodeKey(ctx context.Context, store Store, nodeKey ed25519.PublicKey) ([32]byte, error) {
	peers, _, err := store.Peers(ctx)
	if err != nil {
		return [32]byte{}, err
	}
	for _, p := range peers {
		if bytes.Equal(p.NodeKey, nodeKey) {
			return p.DiscoKey, nil
		}
	}
	return [32]byte{}, errors.New("unregistered node")
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	buf, err := json.Marshal(body)
	if err != nil {
		http.Error(w, "marshal: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.Copy(w, bytes.NewReader(buf))
}
