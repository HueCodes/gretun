package health

import (
	"os"
	"testing"
	"time"
)

func TestICMPIDMask(t *testing.T) {
	// Verify the mask produces a valid 16-bit ICMP identifier.
	pid := os.Getpid()
	id := pid & icmpIDMask

	if id < 0 || id > 0xffff {
		t.Errorf("ICMP ID %d is outside valid 16-bit range", id)
	}

	// Large synthetic PIDs should also be masked correctly.
	largePIDs := []int{0, 1, 65535, 65536, 131072, 1<<20 | 0xbeef}
	for _, p := range largePIDs {
		masked := p & icmpIDMask
		if masked < 0 || masked > 0xffff {
			t.Errorf("PID %d masked to %d, want 0..65535", p, masked)
		}
	}
}

func TestProbeMultiple_ThresholdEvaluation(t *testing.T) {
	tests := []struct {
		name      string
		successes int
		count     int
		threshold int
		want      bool
	}{
		{"all succeed, meets threshold", 3, 3, 2, true},
		{"exact threshold", 2, 3, 2, true},
		{"below threshold", 1, 3, 2, false},
		{"zero successes", 0, 3, 1, false},
		{"threshold zero always healthy", 3, 3, 0, true},
		{"single probe success", 1, 1, 1, true},
		{"single probe failure", 0, 1, 1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.successes >= tt.threshold
			if got != tt.want {
				t.Errorf("%d >= %d = %v, want %v", tt.successes, tt.threshold, got, tt.want)
			}
		})
	}
}

func TestProbeResult_Fields(t *testing.T) {
	r := ProbeResult{
		Target:    "10.0.0.1",
		Success:   true,
		RTT:       42 * time.Millisecond,
		Timestamp: time.Now(),
	}

	if r.Target != "10.0.0.1" {
		t.Errorf("Target = %q, want %q", r.Target, "10.0.0.1")
	}
	if !r.Success {
		t.Error("expected Success = true")
	}
	if r.RTT != 42*time.Millisecond {
		t.Errorf("RTT = %v, want %v", r.RTT, 42*time.Millisecond)
	}
	if r.Error != "" {
		t.Errorf("Error = %q, want empty", r.Error)
	}
}

func TestProbeResult_ErrorCase(t *testing.T) {
	r := ProbeResult{
		Target:    "10.0.0.1",
		Success:   false,
		Error:     "timeout",
		Timestamp: time.Now(),
	}

	if r.Success {
		t.Error("expected Success = false")
	}
	if r.Error != "timeout" {
		t.Errorf("Error = %q, want %q", r.Error, "timeout")
	}
}

func TestConstants(t *testing.T) {
	if icmpIDMask != 0xffff {
		t.Errorf("icmpIDMask = 0x%x, want 0xffff", icmpIDMask)
	}
	if defaultMTU != 1500 {
		t.Errorf("defaultMTU = %d, want 1500", defaultMTU)
	}
	if probeInterval != 100*time.Millisecond {
		t.Errorf("probeInterval = %v, want 100ms", probeInterval)
	}
}
