package commands

import (
	"fmt"
	"net"

	"github.com/HueCodes/gretun/internal/tunnel"
	"github.com/spf13/cobra"
)

var (
	createName     string
	createLocal    string
	createRemote   string
	createKey      uint32
	createTTL      uint8
	createTunnelIP string
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new GRE tunnel",
	Long:  "Create a new GRE tunnel with the specified parameters.",
	Example: `  gretun create --name tun0 --local 10.0.0.1 --remote 10.0.0.2
  gretun create --name tun0 --local 10.0.0.1 --remote 10.0.0.2 --key 12345
  gretun create --name tun0 --local 10.0.0.1 --remote 10.0.0.2 --tunnel-ip 192.168.1.1/30`,
	RunE: runCreate,
}

func init() {
	createCmd.Flags().StringVar(&createName, "name", "", "tunnel interface name (required)")
	createCmd.Flags().StringVar(&createLocal, "local", "", "local endpoint IP (required)")
	createCmd.Flags().StringVar(&createRemote, "remote", "", "remote endpoint IP (required)")
	createCmd.Flags().Uint32Var(&createKey, "key", 0, "GRE key for tunnel identification")
	createCmd.Flags().Uint8Var(&createTTL, "ttl", 64, "TTL for tunnel packets")
	createCmd.Flags().StringVar(&createTunnelIP, "tunnel-ip", "", "IP address to assign to tunnel interface (CIDR notation)")

	createCmd.MarkFlagRequired("name")
	createCmd.MarkFlagRequired("local")
	createCmd.MarkFlagRequired("remote")

	rootCmd.AddCommand(createCmd)
}

func runCreate(cmd *cobra.Command, args []string) error {
	localIP := net.ParseIP(createLocal)
	if localIP == nil {
		return fmt.Errorf("invalid local IP: %s", createLocal)
	}

	remoteIP := net.ParseIP(createRemote)
	if remoteIP == nil {
		return fmt.Errorf("invalid remote IP: %s", createRemote)
	}

	cfg := tunnel.Config{
		Name:     createName,
		LocalIP:  localIP,
		RemoteIP: remoteIP,
		Key:      createKey,
		TTL:      createTTL,
	}

	if err := tunnel.Create(cfg); err != nil {
		return err
	}

	fmt.Printf("created tunnel %s (%s -> %s)\n", createName, createLocal, createRemote)

	if createTunnelIP != "" {
		if err := tunnel.AssignIP(createName, createTunnelIP); err != nil {
			return fmt.Errorf("tunnel created but failed to assign IP: %w", err)
		}
		fmt.Printf("assigned %s to %s\n", createTunnelIP, createName)
	}

	return nil
}
