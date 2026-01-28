package commands

import (
	"fmt"

	"github.com/HueCodes/gretun/internal/tunnel"
	"github.com/spf13/cobra"
)

var deleteName string

var deleteCmd = &cobra.Command{
	Use:     "delete",
	Short:   "Delete a GRE tunnel",
	Long:    "Delete an existing GRE tunnel by name.",
	Example: "  gretun delete --name tun0",
	RunE:    runDelete,
}

func init() {
	deleteCmd.Flags().StringVar(&deleteName, "name", "", "tunnel interface name (required)")
	deleteCmd.MarkFlagRequired("name")

	rootCmd.AddCommand(deleteCmd)
}

func runDelete(cmd *cobra.Command, args []string) error {
	if err := tunnel.Delete(deleteName); err != nil {
		return err
	}

	fmt.Printf("deleted tunnel %s\n", deleteName)
	return nil
}
