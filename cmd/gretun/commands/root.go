package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var jsonOutput bool

var rootCmd = &cobra.Command{
	Use:   "gretun",
	Short: "GRE tunnel manager",
	Long:  "A CLI tool for creating and managing GRE tunnels on Linux.",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if os.Geteuid() != 0 {
			return fmt.Errorf("gretun requires root privileges (run with sudo)")
		}
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "output in JSON format")
}

func Execute() error {
	return rootCmd.Execute()
}
