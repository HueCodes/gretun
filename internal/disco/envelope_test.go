package disco

import (
	"bytes"
	"testing"
)

func TestEnvelope_RoundTrip(t *testing.T) {
	alice, err := GenerateDiscoKey()
	if err != nil {
		t.Fatal(err)
	}
	bob, err := GenerateDiscoKey()
	if err != nil {
		t.Fatal(err)
	}

	in := Body{Type: MsgPing, Tx: "abcd1234", NodeKey: "node-pub-key"}
	env, err := BuildEnvelope(alice, bob.Pub, in)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(env, Magic[:]) {
		t.Fatal("envelope missing magic prefix")
	}

	sender, out, err := OpenEnvelope(env, bob)
	if err != nil {
		t.Fatalf("OpenEnvelope: %v", err)
	}
	if sender != alice.Pub {
		t.Error("sender mismatch")
	}
	if out.Type != in.Type || out.Tx != in.Tx || out.NodeKey != in.NodeKey {
		t.Errorf("body round-trip mismatch: %+v vs %+v", out, in)
	}
}

func TestEnvelope_RejectTampered(t *testing.T) {
	alice, _ := GenerateDiscoKey()
	bob, _ := GenerateDiscoKey()

	env, err := BuildEnvelope(alice, bob.Pub, Body{Type: MsgPong, Tx: "x"})
	if err != nil {
		t.Fatal(err)
	}
	// Flip a byte deep in the ciphertext (not the sender pubkey).
	env[len(env)-1] ^= 0xFF
	if _, _, err := OpenEnvelope(env, bob); err == nil {
		t.Fatal("expected decrypt failure after tampering")
	}
}

func TestEnvelope_RejectBadMagic(t *testing.T) {
	alice, _ := GenerateDiscoKey()
	bob, _ := GenerateDiscoKey()
	env, _ := BuildEnvelope(alice, bob.Pub, Body{Type: MsgPing})
	env[0] = 'X'
	if _, _, err := OpenEnvelope(env, bob); err == nil {
		t.Fatal("expected bad-magic error")
	}
}

func TestEnvelope_RejectWrongRecipient(t *testing.T) {
	alice, _ := GenerateDiscoKey()
	bob, _ := GenerateDiscoKey()
	carol, _ := GenerateDiscoKey()
	env, _ := BuildEnvelope(alice, bob.Pub, Body{Type: MsgCallMeMaybe, Endpoints: []string{"1.2.3.4:5"}})
	if _, _, err := OpenEnvelope(env, carol); err == nil {
		t.Fatal("expected decrypt failure for wrong recipient")
	}
}

func TestKeys_LoadOrCreatePersistent(t *testing.T) {
	dir := t.TempDir()
	nk1, dk1, err := LoadOrCreateKeys(dir)
	if err != nil {
		t.Fatal(err)
	}
	nk2, dk2, err := LoadOrCreateKeys(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(nk1.Priv, nk2.Priv) || !bytes.Equal(nk1.Pub, nk2.Pub) {
		t.Error("node key not stable across loads")
	}
	if dk1.Priv != dk2.Priv || dk1.Pub != dk2.Pub {
		t.Error("disco key not stable across loads")
	}
}
