//go:build linux

package commands

import (
	"context"
	"fmt"
	"log/slog"
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
  gretun create --name tun0 --local 10.0.0.1 --remote 10.0.0.2 --tunnel-ip 192.168.1.1/30
  gretun create --name tun0 --local 10.0.0.1 --remote 10.0.0.2 --encap fou --encap-dport 7777`,
	RunE: runCreate,
}

func init() {
	createCmd.Flags().String("name", "", "tunnel interface name (required)")
	createCmd.Flags().String("local", "", "local endpoint IP (required)")
	createCmd.Flags().String("remote", "", "remote endpoint IP (required)")
	createCmd.Flags().Uint32("key", 0, "GRE key for tunnel identification")
	createCmd.Flags().Uint8("ttl", 64, "TTL for tunnel packets")
	createCmd.Flags().String("tunnel-ip", "", "IP address to assign to tunnel interface (CIDR notation)")

	createCmd.Flags().String("encap", "none", "outer encapsulation: none|fou|gue")
	createCmd.Flags().Uint16("encap-sport", 0, "outer UDP source port (0 = flow-hash)")
	createCmd.Flags().Uint16("encap-dport", 0, "outer UDP destination port (required if --encap != none)")
	createCmd.Flags().Bool("encap-csum", true, "emit UDP checksum on outer packets")
	createCmd.Flags().Int("mtu", 0, "interface MTU (0 = auto; 1468 for FOU/IPv4)")

	_ = createCmd.MarkFlagRequired("name")
	_ = createCmd.MarkFlagRequired("local")
	_ = createCmd.MarkFlagRequired("remote")

	rootCmd.AddCommand(createCmd)
}

func runCreate(cmd *cobra.Command, args []string) error {
	name, _ := cmd.Flags().GetString("name")
	local, _ := cmd.Flags().GetString("local")
	remote, _ := cmd.Flags().GetString("remote")
	key, _ := cmd.Flags().GetUint32("key")
	ttl, _ := cmd.Flags().GetUint8("ttl")
	tunnelIP, _ := cmd.Flags().GetString("tunnel-ip")
	encapStr, _ := cmd.Flags().GetString("encap")
	encapSport, _ := cmd.Flags().GetUint16("encap-sport")
	encapDport, _ := cmd.Flags().GetUint16("encap-dport")
	encapCSum, _ := cmd.Flags().GetBool("encap-csum")
	mtu, _ := cmd.Flags().GetInt("mtu")

	localIP := net.ParseIP(local)
	if localIP == nil {
		return fmt.Errorf("invalid local IP: %s", local)
	}

	remoteIP := net.ParseIP(remote)
	if remoteIP == nil {
		return fmt.Errorf("invalid remote IP: %s", remote)
	}

	encap, err := parseEncap(encapStr)
	if err != nil {
		return err
	}

	cfg := tunnel.Config{
		Name:          name,
		LocalIP:       localIP,
		RemoteIP:      remoteIP,
		Key:           key,
		TTL:           ttl,
		MTU:           mtu,
		Encap:         encap,
		EncapSport:    encapSport,
		EncapDport:    encapDport,
		EncapChecksum: encapCSum,
	}

	if warn, err := tunnel.ValidateEncap(cfg); err != nil {
		return err
	} else if warn != "" {
		slog.Warn(warn)
	}

	ctx := context.Background()

	if err := tunnel.Create(ctx, nl, cfg); err != nil {
		return err
	}

	fmt.Printf("created tunnel %s (%s -> %s)", name, local, remote)
	if encap != tunnel.EncapNone {
		fmt.Printf(" encap=%s dport=%d", encapStr, encapDport)
	}
	fmt.Println()

	if tunnelIP != "" {
		if err := tunnel.AssignIP(ctx, nl, name, tunnelIP); err != nil {
			return fmt.Errorf("tunnel created but failed to assign IP: %w", err)
		}
		fmt.Printf("assigned %s to %s\n", tunnelIP, name)
	}

	return nil
}

func parseEncap(s string) (tunnel.EncapType, error) {
	switch s {
	case "none", "":
		return tunnel.EncapNone, nil
	case "fou":
		return tunnel.EncapFOU, nil
	case "gue":
		return tunnel.EncapGUE, nil
	default:
		return tunnel.EncapNone, fmt.Errorf("unknown --encap value %q (want none|fou|gue)", s)
	}
}
