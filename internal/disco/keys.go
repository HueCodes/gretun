package disco

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/nacl/box"
)

// NodeKey is an Ed25519 identity used to authenticate a peer to the coordinator
// (request signing). It never touches tunnel data and never leaves the node.
type NodeKey struct {
	Priv ed25519.PrivateKey
	Pub  ed25519.PublicKey
}

// DiscoKey is a Curve25519 (X25519) keypair used for NaCl-box encrypting
// disco envelopes. The coordinator sees only the public half so it cannot
// read envelope contents it relays.
type DiscoKey struct {
	Priv [32]byte
	Pub  [32]byte
}

// GenerateNodeKey creates a fresh Ed25519 keypair.
func GenerateNodeKey() (NodeKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return NodeKey{}, err
	}
	return NodeKey{Priv: priv, Pub: pub}, nil
}

// GenerateDiscoKey creates a fresh Curve25519 keypair.
func GenerateDiscoKey() (DiscoKey, error) {
	var k DiscoKey
	if _, err := io.ReadFull(rand.Reader, k.Priv[:]); err != nil {
		return DiscoKey{}, err
	}
	pub, err := curve25519.X25519(k.Priv[:], curve25519.Basepoint)
	if err != nil {
		return DiscoKey{}, err
	}
	copy(k.Pub[:], pub)
	return k, nil
}

// persisted is the on-disk representation. Base64 for readability since these
// files are hand-inspected during debugging.
type persisted struct {
	NodePriv  string `json:"node_priv"`
	NodePub   string `json:"node_pub"`
	DiscoPriv string `json:"disco_priv"`
	DiscoPub  string `json:"disco_pub"`
}

// LoadOrCreateKeys returns the keypairs stored in stateDir, generating and
// persisting fresh ones if none exist. The file is created 0600.
func LoadOrCreateKeys(stateDir string) (NodeKey, DiscoKey, error) {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return NodeKey{}, DiscoKey{}, err
	}
	path := filepath.Join(stateDir, "keys.json")
	b, err := os.ReadFile(path)
	if err == nil {
		var p persisted
		if err := json.Unmarshal(b, &p); err != nil {
			return NodeKey{}, DiscoKey{}, fmt.Errorf("decode %s: %w", path, err)
		}
		nk, err := nodeKeyFromPersisted(p)
		if err != nil {
			return NodeKey{}, DiscoKey{}, err
		}
		dk, err := discoKeyFromPersisted(p)
		if err != nil {
			return NodeKey{}, DiscoKey{}, err
		}
		return nk, dk, nil
	}
	if !os.IsNotExist(err) {
		return NodeKey{}, DiscoKey{}, err
	}

	nk, err := GenerateNodeKey()
	if err != nil {
		return NodeKey{}, DiscoKey{}, err
	}
	dk, err := GenerateDiscoKey()
	if err != nil {
		return NodeKey{}, DiscoKey{}, err
	}

	p := persisted{
		NodePriv:  base64.StdEncoding.EncodeToString(nk.Priv),
		NodePub:   base64.StdEncoding.EncodeToString(nk.Pub),
		DiscoPriv: base64.StdEncoding.EncodeToString(dk.Priv[:]),
		DiscoPub:  base64.StdEncoding.EncodeToString(dk.Pub[:]),
	}
	buf, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return NodeKey{}, DiscoKey{}, err
	}
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		return NodeKey{}, DiscoKey{}, err
	}
	return nk, dk, nil
}

func nodeKeyFromPersisted(p persisted) (NodeKey, error) {
	priv, err := base64.StdEncoding.DecodeString(p.NodePriv)
	if err != nil {
		return NodeKey{}, fmt.Errorf("decode node priv: %w", err)
	}
	pub, err := base64.StdEncoding.DecodeString(p.NodePub)
	if err != nil {
		return NodeKey{}, fmt.Errorf("decode node pub: %w", err)
	}
	if len(priv) != ed25519.PrivateKeySize || len(pub) != ed25519.PublicKeySize {
		return NodeKey{}, fmt.Errorf("bad node key sizes")
	}
	return NodeKey{Priv: priv, Pub: pub}, nil
}

func discoKeyFromPersisted(p persisted) (DiscoKey, error) {
	priv, err := base64.StdEncoding.DecodeString(p.DiscoPriv)
	if err != nil {
		return DiscoKey{}, fmt.Errorf("decode disco priv: %w", err)
	}
	pub, err := base64.StdEncoding.DecodeString(p.DiscoPub)
	if err != nil {
		return DiscoKey{}, fmt.Errorf("decode disco pub: %w", err)
	}
	if len(priv) != 32 || len(pub) != 32 {
		return DiscoKey{}, fmt.Errorf("bad disco key sizes")
	}
	var k DiscoKey
	copy(k.Priv[:], priv)
	copy(k.Pub[:], pub)
	return k, nil
}

// B64 returns a base64-encoded copy of the Ed25519 public key.
func (k NodeKey) B64() string { return base64.StdEncoding.EncodeToString(k.Pub) }

// B64 returns a base64-encoded copy of the disco public key.
func (k DiscoKey) B64() string { return base64.StdEncoding.EncodeToString(k.Pub[:]) }

// DecodeDiscoPub decodes a base64-encoded disco public key.
func DecodeDiscoPub(s string) ([32]byte, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return [32]byte{}, err
	}
	if len(b) != 32 {
		return [32]byte{}, fmt.Errorf("disco pubkey length %d, want 32", len(b))
	}
	var out [32]byte
	copy(out[:], b)
	return out, nil
}

// Seal produces a sealed envelope body addressed to the recipient.
// The return value is opaque: nonce || nacl/box ciphertext.
func Seal(plaintext []byte, recipient [32]byte, sender DiscoKey) ([]byte, error) {
	var nonce [24]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return nil, err
	}
	ct := box.Seal(nonce[:], plaintext, &nonce, &recipient, &sender.Priv)
	return ct, nil
}

// Open reverses Seal.
func Open(sealed []byte, sender [32]byte, recipient DiscoKey) ([]byte, bool) {
	if len(sealed) < 24 {
		return nil, false
	}
	var nonce [24]byte
	copy(nonce[:], sealed[:24])
	return box.Open(nil, sealed[24:], &nonce, &sender, &recipient.Priv)
}
