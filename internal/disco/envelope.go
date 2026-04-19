package disco

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Disco envelope wire format — deliberately mirrors tailscale/disco so the
// reviewer recognises it:
//
//   6 bytes  magic   = "TS" 0xF0 0x9F 0x92 0xAC   (💬 emoji)
//   32 bytes sender  = sender disco pubkey (Curve25519)
//   N bytes  sealed  = 24-byte nonce || nacl/box ciphertext of the body
//
// The body is a JSON object; the `type` field selects one of:
//   - ping          { tx, node_key }
//   - pong          { tx, src }
//   - call_me_maybe { endpoints: [...] }

// Magic is the 6-byte prefix that identifies a disco envelope.
var Magic = [6]byte{'T', 'S', 0xF0, 0x9F, 0x92, 0xAC}

const (
	magicLen    = 6
	pubkeyLen   = 32
	envelopeMin = magicLen + pubkeyLen + 24
)

// MessageType enumerates the disco body messages we speak.
type MessageType string

const (
	MsgPing         MessageType = "ping"
	MsgPong         MessageType = "pong"
	MsgCallMeMaybe  MessageType = "call_me_maybe"
)

// Body is the JSON payload inside the sealed portion of an envelope.
// Individual fields are populated based on Type; unused ones marshal to
// omitempty and are invisible on the wire.
type Body struct {
	Type      MessageType `json:"type"`
	Tx        string      `json:"tx,omitempty"`         // ping/pong: 16-byte hex
	NodeKey   string      `json:"node_key,omitempty"`   // ping: b64 Ed25519 pubkey
	Src       string      `json:"src,omitempty"`        // pong: ip:port the ping was seen from
	Endpoints []string    `json:"endpoints,omitempty"`  // call_me_maybe
}

// Marshal serialises a Body to JSON.
func (b Body) Marshal() ([]byte, error) { return json.Marshal(b) }

// UnmarshalBody parses a JSON-encoded body.
func UnmarshalBody(raw []byte) (Body, error) {
	var b Body
	if err := json.Unmarshal(raw, &b); err != nil {
		return Body{}, err
	}
	return b, nil
}

// BuildEnvelope produces the full wire bytes for a disco envelope addressed
// from sender to recipient. Body is JSON-marshalled and sealed inside.
func BuildEnvelope(sender DiscoKey, recipient [32]byte, body Body) ([]byte, error) {
	plaintext, err := body.Marshal()
	if err != nil {
		return nil, err
	}
	sealed, err := Seal(plaintext, recipient, sender)
	if err != nil {
		return nil, err
	}

	out := make([]byte, 0, magicLen+pubkeyLen+len(sealed))
	out = append(out, Magic[:]...)
	out = append(out, sender.Pub[:]...)
	out = append(out, sealed...)
	return out, nil
}

// ParseEnvelope extracts the sender pubkey and sealed body from a wire buffer.
// It does NOT decrypt. Call OpenEnvelope for the plaintext body.
func ParseEnvelope(buf []byte) (sender [32]byte, sealed []byte, err error) {
	if len(buf) < envelopeMin {
		return sender, nil, fmt.Errorf("envelope too short (%d bytes)", len(buf))
	}
	if [6]byte(buf[:magicLen]) != Magic {
		return sender, nil, errors.New("envelope: bad magic")
	}
	copy(sender[:], buf[magicLen:magicLen+pubkeyLen])
	sealed = buf[magicLen+pubkeyLen:]
	return sender, sealed, nil
}

// OpenEnvelope parses and decrypts a wire envelope with the given recipient
// disco key, returning the sender's pubkey and decoded body.
func OpenEnvelope(buf []byte, recipient DiscoKey) (sender [32]byte, body Body, err error) {
	sender, sealed, err := ParseEnvelope(buf)
	if err != nil {
		return sender, Body{}, err
	}
	plain, ok := Open(sealed, sender, recipient)
	if !ok {
		return sender, Body{}, errors.New("envelope: decrypt failed")
	}
	body, err = UnmarshalBody(plain)
	return sender, body, err
}
