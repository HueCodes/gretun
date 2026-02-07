package commands

import (
	"fmt"
	"net"

	"github.com/HueCodes/gretun/internal/tunnel"
	"github.com/spf13/cobra"
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
	createCmd.Flags().String("name", "", "tunnel interface name (required)")
	createCmd.Flags().String("local", "", "local endpoint IP (required)")
	createCmd.Flags().String("remote", "", "remote endpoint IP (required)")
	createCmd.Flags().Uint32("key", 0, "GRE key for tunnel identification")
	createCmd.Flags().Uint8("ttl", 64, "TTL for tunnel packets")
	createCmd.Flags().String("tunnel-ip", "", "IP address to assign to tunnel interface (CIDR notation)")

	createCmd.MarkFlagRequired("name")
	createCmd.MarkFlagRequired("local")
	createCmd.MarkFlagRequired("remote")

	rootCmd.AddCommand(createCmd)
}

func runCreate(cmd *cobra.Command, args []string) error {
	name, _ := cmd.Flags().GetString("name")
	local, _ := cmd.Flags().GetString("local")
	remote, _ := cmd.Flags().GetString("remote")
	key, _ := cmd.Flags().GetUint32("key")
	ttl, _ := cmd.Flags().GetUint8("ttl")
	tunnelIP, _ := cmd.Flags().GetString("tunnel-ip")

	localIP := net.ParseIP(local)
	if localIP == nil {
		return fmt.Errorf("invalid local IP: %s", local)
	}

	remoteIP := net.ParseIP(remote)
	if remoteIP == nil {
		return fmt.Errorf("invalid remote IP: %s", remote)
	}

	cfg := tunnel.Config{
		Name:     name,
		LocalIP:  localIP,
		RemoteIP: remoteIP,
		Key:      key,
		TTL:      ttl,
	}

	if err := tunnel.Create(nl, cfg); err != nil {
		return err
	}

	fmt.Printf("created tunnel %s (%s -> %s)\n", name, local, remote)

	if tunnelIP != "" {
		if err := tunnel.AssignIP(nl, name, tunnelIP); err != nil {
			return fmt.Errorf("tunnel created but failed to assign IP: %w", err)
		}
		fmt.Printf("assigned %s to %s\n", tunnelIP, name)
	}

	return nil
}
