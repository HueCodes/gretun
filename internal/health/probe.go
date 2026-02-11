package health

import (
	"context"
	"fmt"
	"net"
	"os"
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
)

// ProbeResult contains the result of a health probe.
type ProbeResult struct {
	Target    string        `json:"target"`
	Success   bool          `json:"success"`
	RTT       time.Duration `json:"rtt_ms,omitempty"`
	Error     string        `json:"error,omitempty"`
	Timestamp time.Time     `json:"timestamp"`
}

// Probe sends an ICMP echo request to the target and waits for a reply.
func Probe(ctx context.Context, target string, timeout time.Duration) ProbeResult {
	result := ProbeResult{
		Target:    target,
		Timestamp: time.Now(),
	}

	// Check for cancellation
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

	msg := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   os.Getpid() & icmpIDMask,
			Seq:  1,
			Data: []byte("gretun-probe"),
		},
	}

	msgBytes, err := msg.Marshal(nil)
	if err != nil {
		result.Error = fmt.Sprintf("failed to marshal ICMP: %v", err)
		return result
	}

	start := time.Now()

	if _, err := conn.WriteTo(msgBytes, dst); err != nil {
		result.Error = fmt.Sprintf("failed to send ICMP: %v", err)
		return result
	}

	// Use context deadline if it's sooner than the timeout
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
		// Check if context was cancelled
		select {
		case <-ctx.Done():
			result.Error = fmt.Sprintf("cancelled: %v", ctx.Err())
		default:
			result.Error = fmt.Sprintf("failed to receive reply: %v", err)
		}
		return result
	}

	rtt := time.Since(start)

	parsed, err := icmp.ParseMessage(1, reply[:n])
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

// ProbeMultiple sends multiple probes and returns success if threshold is met.
func ProbeMultiple(ctx context.Context, target string, count int, timeout time.Duration, threshold int) (bool, []ProbeResult) {
	results := make([]ProbeResult, 0, count)
	successes := 0

	for i := 0; i < count; i++ {
		// Check for cancellation before each probe
		select {
		case <-ctx.Done():
			// Return early if cancelled
			return false, results
		default:
		}

		result := Probe(ctx, target, timeout)
		results = append(results, result)
		if result.Success {
			successes++
		}
		if i < count-1 {
			// Use context-aware sleep
			select {
			case <-time.After(probeInterval):
			case <-ctx.Done():
				return false, results
			}
		}
	}

	return successes >= threshold, results
}
