package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/HueCodes/gretun/internal/tunnel"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:     "list",
	Short:   "List all GRE tunnels",
	Long:    "List all GRE tunnels on the system.",
	Example: "  gretun list",
	RunE:    runList,
}

func init() {
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	tunnels, err := tunnel.List()
	if err != nil {
		return err
	}

	if len(tunnels) == 0 {
		fmt.Println("no GRE tunnels found")
		return nil
	}

	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(tunnels)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tLOCAL\tREMOTE\tKEY\tTUNNEL IP\tSTATUS")

	for _, t := range tunnels {
		status := "down"
		if t.Up {
			status = "up"
		}
		key := "-"
		if t.Key != 0 {
			key = fmt.Sprintf("%d", t.Key)
		}
		tunnelIP := "-"
		if t.TunnelIP != "" {
			tunnelIP = t.TunnelIP
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			t.Name, t.LocalIP, t.RemoteIP, key, tunnelIP, status)
	}

	return w.Flush()
}
