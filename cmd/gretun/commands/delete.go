package commands

import (
	"fmt"

	"github.com/HueCodes/gretun/internal/tunnel"
	"github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
	Use:     "delete",
	Short:   "Delete a GRE tunnel",
	Long:    "Delete an existing GRE tunnel by name.",
	Example: "  gretun delete --name tun0",
	RunE:    runDelete,
}

func init() {
	deleteCmd.Flags().String("name", "", "tunnel interface name (required)")
	deleteCmd.MarkFlagRequired("name")

	rootCmd.AddCommand(deleteCmd)
}

func runDelete(cmd *cobra.Command, args []string) error {
	name, _ := cmd.Flags().GetString("name")

	if err := tunnel.Delete(nl, name); err != nil {
		return err
	}

	fmt.Printf("deleted tunnel %s\n", name)
	return nil
}
