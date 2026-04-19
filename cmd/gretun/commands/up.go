//go:build linux

package commands

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/HueCodes/gretun/internal/daemon"
	"github.com/HueCodes/gretun/internal/disco"
	"github.com/spf13/cobra"
)

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Bring up the gretun daemon: register with a coordinator and punch holes",
	Long: `Long-lived daemon. Loads or generates node + disco keypairs from
--state-dir, opens a disco UDP socket, STUN-discovers this host's public
endpoint, registers with the given coordinator, and brings up GRE-over-FOU
tunnels to each reachable peer. SIGINT/SIGTERM tears everything down.`,
	Example: `  sudo gretun up --coordinator http://coord.example.com:8443
  sudo gretun up --coordinator https://coord.example.com --node-name site-a --fou-port 7777`,
	RunE: runUp,
}

func init() {
	host, _ := os.Hostname()
	def := filepath.Join(os.Getenv("HOME"), ".config", "gretun")

	upCmd.Flags().String("coordinator", "", "coordinator URL (required)")
	upCmd.Flags().String("iface", "gretun%d", "interface name pattern (%d → peer index)")
	upCmd.Flags().Uint16("fou-port", 7777, "kernel FOU RX port for GRE-over-UDP")
	upCmd.Flags().String("node-name", host, "human-readable node name")
	upCmd.Flags().String("state-dir", def, "directory for persistent keys")
	upCmd.Flags().Bool("aggressive-punch", false, "use 256-port probing for symmetric NAT (experimental)")
	upCmd.Flags().StringSlice("stun-server", nil, "STUN server host:port (repeatable)")

	_ = upCmd.MarkFlagRequired("coordinator")
	rootCmd.AddCommand(upCmd)
}

func runUp(cmd *cobra.Command, args []string) error {
	coordURL, _ := cmd.Flags().GetString("coordinator")
	iface, _ := cmd.Flags().GetString("iface")
	fouPort, _ := cmd.Flags().GetUint16("fou-port")
	name, _ := cmd.Flags().GetString("node-name")
	stateDir, _ := cmd.Flags().GetString("state-dir")
	aggressive, _ := cmd.Flags().GetBool("aggressive-punch")
	stunServers, _ := cmd.Flags().GetStringSlice("stun-server")

	nk, dk, err := disco.LoadOrCreateKeys(stateDir)
	if err != nil {
		return fmt.Errorf("load keys: %w", err)
	}

	d := daemon.New(daemon.Config{
		Coordinator: coordURL,
		NodeName:    name,
		StateDir:    stateDir,
		Iface:       iface,
		FOUPort:     fouPort,
		STUNServers: stunServers,
		Aggressive:  aggressive,
	}, nl, nk, dk)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	return d.Run(ctx)
}
