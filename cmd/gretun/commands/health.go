//go:build linux

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"text/tabwriter"
	"time"

	"github.com/HueCodes/gretun/internal/health"
	"github.com/HueCodes/gretun/internal/tunnel"
	"github.com/spf13/cobra"
)

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Check health of all GRE tunnels",
	Long:  "Probe all GRE tunnels via ICMP and display a health table.",
	Example: `  gretun health
  gretun health --watch
  gretun health --watch --interval 10s
  gretun health --concurrency 3
  gretun health --json`,
	RunE: runHealth,
}

func init() {
	healthCmd.Flags().Bool("watch", false, "repeat probe on a ticker")
	healthCmd.Flags().Duration("interval", 5*time.Second, "interval between probes in watch mode")
	healthCmd.Flags().Int("concurrency", 5, "number of concurrent ICMP goroutines")
	healthCmd.Flags().Duration("timeout", 2*time.Second, "per-probe timeout")

	rootCmd.AddCommand(healthCmd)
}

// healthRow is a single row of health output used for both table and JSON rendering.
type healthRow struct {
	Name    string  `json:"name"`
	Remote  string  `json:"remote"`
	Healthy bool    `json:"healthy"`
	RTTMS   float64 `json:"rtt_ms"`
}

func runHealth(cmd *cobra.Command, args []string) error {
	watch, _ := cmd.Flags().GetBool("watch")
	interval, _ := cmd.Flags().GetDuration("interval")
	concurrency, _ := cmd.Flags().GetInt("concurrency")
	timeout, _ := cmd.Flags().GetDuration("timeout")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	probe := func() ([]healthRow, error) {
		tunnels, err := tunnel.List(ctx, nl)
		if err != nil {
			return nil, err
		}

		targets := make([]string, 0, len(tunnels))
		for _, t := range tunnels {
			if t.RemoteIP != "" {
				targets = append(targets, t.RemoteIP)
			}
		}

		results := health.ProbeTargets(ctx, targets, timeout, concurrency)

		rows := make([]healthRow, 0, len(tunnels))
		for _, t := range tunnels {
			row := healthRow{
				Name:   t.Name,
				Remote: t.RemoteIP,
			}
			if r, ok := results[t.RemoteIP]; ok {
				row.Healthy = r.Success
				if r.Success {
					row.RTTMS = float64(r.RTT) / float64(time.Millisecond)
				}
			}
			rows = append(rows, row)
		}

		return rows, nil
	}

	printTable := func(rows []healthRow) {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tREMOTE\tSTATUS\tRTT")
		for _, row := range rows {
			status := "unhealthy"
			if row.Healthy {
				status = "healthy"
			}
			rtt := "-"
			if row.Healthy {
				rtt = fmt.Sprintf("%.2fms", row.RTTMS)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", row.Name, row.Remote, status, rtt)
		}
		w.Flush()
	}

	printJSON := func(rows []healthRow) error {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}

	if !watch {
		rows, err := probe()
		if err != nil {
			return err
		}
		if jsonOutput {
			return printJSON(rows)
		}
		printTable(rows)
		return nil
	}

	// Watch mode: probe on a ticker, refreshing the display each interval.
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run an immediate probe before waiting for the first tick.
	rows, err := probe()
	if err != nil {
		return err
	}
	fmt.Print("\033[H\033[2J")
	if jsonOutput {
		if err := printJSON(rows); err != nil {
			return err
		}
	} else {
		printTable(rows)
	}

	for {
		select {
		case <-ctx.Done():
			fmt.Println("interrupted")
			return nil
		case <-ticker.C:
			rows, err := probe()
			if err != nil {
				// If the context was cancelled, the error is expected.
				select {
				case <-ctx.Done():
					fmt.Println("interrupted")
					return nil
				default:
				}
				return err
			}
			fmt.Print("\033[H\033[2J")
			if jsonOutput {
				if err := printJSON(rows); err != nil {
					return err
				}
			} else {
				printTable(rows)
			}
		}
	}
}
