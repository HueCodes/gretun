//go:build linux

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/HueCodes/gretun/internal/disco"
	"github.com/spf13/cobra"
)

var stunCmd = &cobra.Command{
	Use:   "stun",
	Short: "Print this host's public UDP endpoint via STUN",
	Long: `Open an IPv4 UDP socket, issue a STUN binding request to one or more
STUN servers in parallel, and print the XOR-MAPPED-ADDRESS of the first
response. This is the same discovery the daemon does internally; exposed as
a command so it can be used to sanity-check NAT behaviour.`,
	Example: `  gretun stun
  gretun stun --server stun.cloudflare.com:3478 --json`,
	RunE: runStun,
}

func init() {
	stunCmd.Flags().StringSlice("server", nil, "STUN server host:port (repeatable); defaults to pion/Cloudflare/Google")
	stunCmd.Flags().Duration("timeout", 5*time.Second, "overall timeout")
	// STUN doesn't need CAP_NET_ADMIN; bypass the root PersistentPreRunE that
	// installs the netlink handle and capability check.
	stunCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error { return nil }
	rootCmd.AddCommand(stunCmd)
}

func runStun(cmd *cobra.Command, args []string) error {
	servers, _ := cmd.Flags().GetStringSlice("server")
	timeout, _ := cmd.Flags().GetDuration("timeout")

	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return fmt.Errorf("listen udp4: %w", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ep, err := disco.DiscoverPublic(ctx, conn, servers)
	if err != nil {
		return err
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(struct {
			Addr string `json:"addr"`
			Via  string `json:"via"`
		}{Addr: ep.Addr.String(), Via: ep.Via})
	}

	fmt.Printf("%s  (via %s)\n", ep.Addr.String(), ep.Via)
	return nil
}
