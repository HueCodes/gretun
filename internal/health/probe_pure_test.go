package health

import (
	"context"
	"encoding/binary"
	"os"
	"testing"
	"time"
)

func TestIcmpChecksum_KnownVector(t *testing.T) {
	// RFC 1071 example from Section 3: 16-bit ones-complement sum of
	// 0001, f203, f4f5, f6f7.  The one's complement of the sum = 0x220d.
	b := []byte{
		0x00, 0x01,
		0xf2, 0x03,
		0xf4, 0xf5,
		0xf6, 0xf7,
	}
	got := icmpChecksum(b)
	if got != 0x220d {
		t.Errorf("icmpChecksum = 0x%04x, want 0x220d", got)
	}
}

func TestIcmpChecksum_OddLengthNoPanic(t *testing.T) {
	b := []byte{1, 2, 3}
	_ = icmpChecksum(b) // must not panic on odd length
}

func TestIcmpChecksum_AllZeroes(t *testing.T) {
	if got := icmpChecksum(make([]byte, 16)); got != 0xffff {
		t.Errorf("all-zero checksum = 0x%04x, want 0xffff (one's complement of 0)", got)
	}
}

func TestIcmpChecksum_VerifiesSelf(t *testing.T) {
	// Invariant: sum of (message with checksum) = 0xffff → one's complement = 0.
	// Build an echo request, then verify the whole message sums to 0xffff when
	// the stored checksum is preserved.
	var buf [icmpEchoSize]byte
	buildEchoRequest(buf[:])

	// Sum the bytes just like icmpChecksum, but WITHOUT taking one's complement.
	var sum uint32
	for i := 0; i+1 < len(buf); i += 2 {
		sum += uint32(buf[i])<<8 | uint32(buf[i+1])
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	// With the checksum field populated, the running sum should be 0xffff.
	if uint16(sum) != 0xffff {
		t.Errorf("self-verify sum = 0x%04x, want 0xffff", uint16(sum))
	}
}

func TestBuildEchoRequest_Fields(t *testing.T) {
	var buf [icmpEchoSize]byte
	out := buildEchoRequest(buf[:])
	if len(out) != icmpEchoSize {
		t.Fatalf("length = %d, want %d", len(out), icmpEchoSize)
	}

	if out[0] != icmpTypeEchoRequest {
		t.Errorf("type = %d, want %d", out[0], icmpTypeEchoRequest)
	}
	if out[1] != 0 {
		t.Errorf("code = %d, want 0", out[1])
	}
	wantID := uint16(os.Getpid() & icmpIDMask)
	if got := binary.BigEndian.Uint16(out[4:6]); got != wantID {
		t.Errorf("id = %d, want %d", got, wantID)
	}
	if got := binary.BigEndian.Uint16(out[6:8]); got != icmpSequence {
		t.Errorf("seq = %d, want %d", got, icmpSequence)
	}
	if string(out[8:8+len(probeData)]) != string(probeData) {
		t.Errorf("payload wrong: %q", out[8:])
	}
	// Checksum field is non-zero.
	if binary.BigEndian.Uint16(out[2:4]) == 0 {
		t.Error("checksum should not be 0 after build")
	}
}

func TestBuildEchoRequest_Idempotent(t *testing.T) {
	var a, b [icmpEchoSize]byte
	buildEchoRequest(a[:])
	buildEchoRequest(b[:])
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("byte %d differs: 0x%02x vs 0x%02x", i, a[i], b[i])
		}
	}
}

// Probe() itself requires a raw ICMP socket which needs root/CAP_NET_RAW.
// We can still exercise its early return paths safely.

func TestProbe_ImmediateCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := Probe(ctx, "127.0.0.1", 100*time.Millisecond)
	if r.Success {
		t.Error("cancelled context should not yield Success")
	}
	if r.Error == "" {
		t.Error("should record an error message")
	}
}

func TestProbe_BadTargetResolution(t *testing.T) {
	// If we can open the socket (root), an invalid target will fail DNS/parse.
	// If we cannot open the socket (no CAP_NET_RAW), we still exercise the
	// listen-failure branch. Either way the result records an error string.
	r := Probe(context.Background(), "definitely.invalid.local.", 100*time.Millisecond)
	if r.Success {
		t.Error("bogus target should not succeed")
	}
	if r.Error == "" {
		t.Error("expected error string for bogus target")
	}
}

func TestProbeMultiple_ZeroCount(t *testing.T) {
	ok, results := ProbeMultiple(context.Background(), "1.1.1.1", 0, time.Second, 0)
	// 0 count, 0 threshold → 0 successes which is >= 0 threshold → ok=true with empty results.
	if !ok {
		t.Error("threshold 0 with count 0 should be healthy")
	}
	if len(results) != 0 {
		t.Errorf("want 0 results, got %d", len(results))
	}
}

func TestProbeMultiple_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ok, _ := ProbeMultiple(ctx, "1.1.1.1", 3, time.Second, 1)
	if ok {
		t.Error("cancelled context should not be healthy")
	}
}

func TestProbeTargets_NonEmpty(t *testing.T) {
	// Use an already-expired context so we exercise the dispatch loop without
	// actually sending ICMP. Targets should all record errors.
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	targets := []string{"1.2.3.4", "5.6.7.8", "9.10.11.12"}
	results := ProbeTargets(ctx, targets, 10*time.Millisecond, 2)

	// Each target either got a cancelled result or was skipped. We don't
	// require all three; we do require every recorded result to be a failure.
	for target, r := range results {
		if r.Success {
			t.Errorf("target %s: expected failure under expired context", target)
		}
		if r.Target != target {
			t.Errorf("target string not round-tripped: got %q, key %q", r.Target, target)
		}
	}
}
