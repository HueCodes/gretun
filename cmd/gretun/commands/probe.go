package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/HueCodes/gretun/internal/health"
	"github.com/spf13/cobra"
)

var (
	probeTarget    string
	probeCount     int
	probeTimeout   time.Duration
	probeThreshold int
)

var probeCmd = &cobra.Command{
	Use:   "probe",
	Short: "Probe tunnel connectivity",
	Long:  "Send ICMP probes through the tunnel to verify connectivity.",
	Example: `  gretun probe --target 192.168.1.2
  gretun probe --target 192.168.1.2 --count 5 --threshold 3`,
	RunE: runProbe,
}

func init() {
	probeCmd.Flags().StringVar(&probeTarget, "target", "", "target IP to probe (required)")
	probeCmd.Flags().IntVar(&probeCount, "count", 3, "number of probes to send")
	probeCmd.Flags().DurationVar(&probeTimeout, "timeout", 2*time.Second, "timeout per probe")
	probeCmd.Flags().IntVar(&probeThreshold, "threshold", 2, "minimum successful probes for healthy status")

	probeCmd.MarkFlagRequired("target")

	rootCmd.AddCommand(probeCmd)
}

func runProbe(cmd *cobra.Command, args []string) error {
	healthy, results := health.ProbeMultiple(probeTarget, probeCount, probeTimeout, probeThreshold)

	if jsonOutput {
		output := struct {
			Target    string               `json:"target"`
			Healthy   bool                 `json:"healthy"`
			Threshold int                  `json:"threshold"`
			Results   []health.ProbeResult `json:"results"`
		}{
			Target:    probeTarget,
			Healthy:   healthy,
			Threshold: probeThreshold,
			Results:   results,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(output)
	}

	successes := 0
	for i, r := range results {
		if r.Success {
			successes++
			fmt.Printf("probe %d: %s rtt=%v\n", i+1, probeTarget, r.RTT.Round(time.Microsecond))
		} else {
			fmt.Printf("probe %d: %s error=%s\n", i+1, probeTarget, r.Error)
		}
	}

	fmt.Printf("\n%d/%d probes successful\n", successes, probeCount)

	if healthy {
		fmt.Println("status: healthy")
	} else {
		fmt.Println("status: unhealthy")
		return fmt.Errorf("probe failed: only %d/%d successful (threshold: %d)", successes, probeCount, probeThreshold)
	}

	return nil
}
