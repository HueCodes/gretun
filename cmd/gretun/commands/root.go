package commands

import (
	"log/slog"
	"os"

	"github.com/HueCodes/gretun/internal/capabilities"
	"github.com/HueCodes/gretun/internal/tunnel"
	"github.com/spf13/cobra"
)

var (
	jsonOutput bool
	nl         = tunnel.DefaultNetlinker{}
)

var rootCmd = &cobra.Command{
	Use:   "gretun",
	Short: "GRE tunnel manager",
	Long:  "A CLI tool for creating and managing GRE tunnels on Linux.",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// version doesn't need root
		if cmd.Name() == "version" {
			return nil
		}

		verbose, _ := cmd.Flags().GetBool("verbose")
		if verbose {
			slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelDebug,
			})))
		}

		// Check for required network administration capabilities
		return capabilities.CheckNetAdmin()
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "output in JSON format")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "enable verbose logging to stderr")
}

func Execute() error {
	return rootCmd.Execute()
}
