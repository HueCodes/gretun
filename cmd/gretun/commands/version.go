package commands

import (
	"fmt"

	"github.com/HueCodes/gretun/internal/version"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("gretun %s (commit: %s, built: %s)\n",
			version.Version, version.Commit, version.BuildTime)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
