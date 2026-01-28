package commands

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/HueCodes/gretun/internal/tunnel"
	"github.com/spf13/cobra"
)

var statusName string

var statusCmd = &cobra.Command{
	Use:     "status",
	Short:   "Show status of a GRE tunnel",
	Long:    "Show detailed status of a specific GRE tunnel.",
	Example: "  gretun status --name tun0",
	RunE:    runStatus,
}

func init() {
	statusCmd.Flags().StringVar(&statusName, "name", "", "tunnel interface name (required)")
	statusCmd.MarkFlagRequired("name")

	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	status, err := tunnel.Get(statusName)
	if err != nil {
		return err
	}

	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(status)
	}

	state := "down"
	if status.Up {
		state = "up"
	}

	fmt.Printf("Tunnel: %s\n", status.Name)
	fmt.Printf("  Status:    %s\n", state)
	fmt.Printf("  Local:     %s\n", status.LocalIP)
	fmt.Printf("  Remote:    %s\n", status.RemoteIP)
	if status.Key != 0 {
		fmt.Printf("  Key:       %d\n", status.Key)
	}
	fmt.Printf("  TTL:       %d\n", status.TTL)
	if status.TunnelIP != "" {
		fmt.Printf("  Tunnel IP: %s\n", status.TunnelIP)
	}

	return nil
}
