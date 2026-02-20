// Package health provides ICMP-based host reachability probing for gretun.
package health

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

const (
	// icmpIDMask masks the PID to a valid 16-bit ICMP identifier.
	icmpIDMask = 0xffff

	// defaultMTU is the standard Ethernet MTU used as the reply buffer size.
	defaultMTU = 1500

	// probeInterval is the delay between consecutive probes.
	probeInterval = 100 * time.Millisecond

	// icmpEchoSize is the fixed byte length of an ICMPv4 echo request carrying
	// the "gretun-probe" payload: 8-byte ICMP header + 12-byte payload.
	icmpEchoSize = 20

	// icmpTypeEchoRequest is the ICMPv4 message type for echo request (RFC 792).
	icmpTypeEchoRequest = 8

	// icmpProtocol is the IP protocol number for ICMPv4 (IANA assigned).
	icmpProtocol = 1

	// icmpSequence is the sequence number embedded in every outgoing echo request.
	icmpSequence = 1
)

// probeData is the static payload embedded in every echo request.
var probeData = []byte("gretun-probe")

// ProbeResult contains the result of a single ICMP health probe.
type ProbeResult struct {
	// Target is the host or IP address that was probed.
	Target string `json:"target"`

	// Success is true when an ICMP echo reply was received within the timeout.
	Success bool `json:"success"`

	// RTT is the round-trip time measured from send to receive.
	RTT time.Duration `json:"rtt_ms,omitempty"`

	// Error holds a human-readable description of any failure.
	Error string `json:"error,omitempty"`

	// Timestamp records when the probe was initiated.
	Timestamp time.Time `json:"timestamp"`
}

// buildEchoRequest writes a marshalled ICMPv4 echo request into dst, which
// must be at least icmpEchoSize bytes long. It returns the slice of dst that
// was written (always dst[:icmpEchoSize]).
//
// Constructing the bytes directly avoids the heap allocation that
// icmp.Message.Marshal(nil) would otherwise cause on every call.
func buildEchoRequest(dst []byte) []byte {
	b := dst[:icmpEchoSize]

	// Type=8 (echo request), Code=0
	b[0] = icmpTypeEchoRequest
	b[1] = 0

	// Checksum placeholder (filled in below)
	b[2] = 0
	b[3] = 0

	// Identifier: lower 16 bits of PID
	id := uint16(os.Getpid() & icmpIDMask)
	binary.BigEndian.PutUint16(b[4:6], id)

	// Sequence number: 1
	binary.BigEndian.PutUint16(b[6:8], icmpSequence)

	// Payload
	copy(b[8:], probeData)

	// Compute RFC 792 checksum over the full message.
	binary.BigEndian.PutUint16(b[2:4], icmpChecksum(b))

	return b
}

// icmpChecksum computes the Internet checksum (RFC 1071) for b.
func icmpChecksum(b []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(b); i += 2 {
		sum += uint32(b[i])<<8 | uint32(b[i+1])
	}
	if len(b)%2 != 0 {
		sum += uint32(b[len(b)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

// Probe sends a single ICMP echo request to target and waits for a reply.
// timeout controls the per-probe deadline; ctx cancellation is also respected.
func Probe(ctx context.Context, target string, timeout time.Duration) ProbeResult {
	result := ProbeResult{
		Target:    target,
		Timestamp: time.Now(),
	}

	// Check for cancellation before doing any work.
	select {
	case <-ctx.Done():
		result.Error = fmt.Sprintf("cancelled: %v", ctx.Err())
		return result
	default:
	}

	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		result.Error = fmt.Sprintf("failed to listen: %v", err)
		return result
	}
	defer conn.Close()

	dst, err := net.ResolveIPAddr("ip4", target)
	if err != nil {
		result.Error = fmt.Sprintf("failed to resolve %s: %v", target, err)
		return result
	}

	// Build the ICMP echo request into a stack-allocated-sized array to avoid
	// the heap allocation that icmp.Message.Marshal(nil) would produce.
	var msgBuf [icmpEchoSize]byte
	msgBytes := buildEchoRequest(msgBuf[:])

	start := time.Now()

	if _, err := conn.WriteTo(msgBytes, dst); err != nil {
		result.Error = fmt.Sprintf("failed to send ICMP: %v", err)
		return result
	}

	// Use whichever deadline is sooner: the caller's timeout or the context deadline.
	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}

	if err := conn.SetReadDeadline(deadline); err != nil {
		result.Error = fmt.Sprintf("failed to set deadline: %v", err)
		return result
	}

	reply := make([]byte, defaultMTU)
	n, _, err := conn.ReadFrom(reply)
	if err != nil {
		select {
		case <-ctx.Done():
			result.Error = fmt.Sprintf("cancelled: %v", ctx.Err())
		default:
			result.Error = fmt.Sprintf("failed to receive reply: %v", err)
		}
		return result
	}

	rtt := time.Since(start)

	parsed, err := icmp.ParseMessage(icmpProtocol, reply[:n])
	if err != nil {
		result.Error = fmt.Sprintf("failed to parse reply: %v", err)
		return result
	}

	if parsed.Type != ipv4.ICMPTypeEchoReply {
		result.Error = fmt.Sprintf("unexpected ICMP type: %v", parsed.Type)
		return result
	}

	result.Success = true
	result.RTT = rtt

	return result
}

// ProbeMultiple sends count sequential probes to a single target and returns
// true when at least threshold of them succeed. An interval of probeInterval
// is inserted between probes to avoid burst traffic. Context cancellation
// causes an early return with whatever results have been collected so far.
func ProbeMultiple(ctx context.Context, target string, count int, timeout time.Duration, threshold int) (bool, []ProbeResult) {
	results := make([]ProbeResult, 0, count)
	successes := 0

	for i := 0; i < count; i++ {
		// Check for cancellation before each probe.
		select {
		case <-ctx.Done():
			return false, results
		default:
		}

		result := Probe(ctx, target, timeout)
		results = append(results, result)
		if result.Success {
			successes++
		}
		if i < count-1 {
			// Context-aware inter-probe sleep.
			select {
			case <-time.After(probeInterval):
			case <-ctx.Done():
				return false, results
			}
		}
	}

	return successes >= threshold, results
}

// ProbeTargets probes multiple targets concurrently using a bounded worker pool.
// concurrency controls how many goroutines run simultaneously; if concurrency
// is <= 0 or greater than len(targets), it defaults to len(targets) so that
// every target gets its own goroutine (no artificial bound for small lists).
//
// Context cancellation is propagated: targets not yet dispatched are skipped
// when ctx is done. The function always waits for all in-flight goroutines to
// finish before returning, so there are no goroutine leaks.
//
// The returned map uses each target string as a key.
func ProbeTargets(ctx context.Context, targets []string, timeout time.Duration, concurrency int) map[string]ProbeResult {
	n := len(targets)
	if n == 0 {
		return map[string]ProbeResult{}
	}

	if concurrency <= 0 || concurrency > n {
		concurrency = n
	}

	// Semaphore: a buffered channel of empty structs bounds goroutine parallelism.
	sem := make(chan struct{}, concurrency)

	results := make(map[string]ProbeResult, n)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, t := range targets {
		// Check for context cancellation before dispatching each goroutine.
		select {
		case <-ctx.Done():
			// Skip remaining targets; already-launched goroutines finish normally.
			goto wait
		default:
		}

		wg.Add(1)
		go func(target string) {
			defer wg.Done()

			// Acquire a semaphore slot; block until one is free or ctx is done.
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				// Record a cancelled result without calling Probe.
				r := ProbeResult{
					Target:    target,
					Timestamp: time.Now(),
					Error:     fmt.Sprintf("cancelled: %v", ctx.Err()),
				}
				mu.Lock()
				results[target] = r
				mu.Unlock()
				return
			}
			defer func() { <-sem }()

			r := Probe(ctx, target, timeout)

			mu.Lock()
			results[target] = r
			mu.Unlock()
		}(t)
	}

wait:
	wg.Wait()
	return results
}
