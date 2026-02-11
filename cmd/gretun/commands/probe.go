package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/HueCodes/gretun/internal/health"
	"github.com/spf13/cobra"
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
	probeCmd.Flags().String("target", "", "target IP to probe (required)")
	probeCmd.Flags().Int("count", 3, "number of probes to send")
	probeCmd.Flags().Duration("timeout", 2*time.Second, "timeout per probe")
	probeCmd.Flags().Int("threshold", 2, "minimum successful probes for healthy status")

	_ = probeCmd.MarkFlagRequired("target")

	rootCmd.AddCommand(probeCmd)
}

func runProbe(cmd *cobra.Command, args []string) error {
	target, _ := cmd.Flags().GetString("target")
	count, _ := cmd.Flags().GetInt("count")
	timeout, _ := cmd.Flags().GetDuration("timeout")
	threshold, _ := cmd.Flags().GetInt("threshold")

	ctx := context.Background()

	healthy, results := health.ProbeMultiple(ctx, target, count, timeout, threshold)

	if jsonOutput {
		output := struct {
			Target    string               `json:"target"`
			Healthy   bool                 `json:"healthy"`
			Threshold int                  `json:"threshold"`
			Results   []health.ProbeResult `json:"results"`
		}{
			Target:    target,
			Healthy:   healthy,
			Threshold: threshold,
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
			fmt.Printf("probe %d: %s rtt=%v\n", i+1, target, r.RTT.Round(time.Microsecond))
		} else {
			fmt.Printf("probe %d: %s error=%s\n", i+1, target, r.Error)
		}
	}

	fmt.Printf("\n%d/%d probes successful\n", successes, count)

	if healthy {
		fmt.Println("status: healthy")
	} else {
		fmt.Println("status: unhealthy")
		return fmt.Errorf("probe failed: only %d/%d successful (threshold: %d)", successes, count, threshold)
	}

	return nil
}
