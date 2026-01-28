package main

import (
	"os"

	"github.com/HueCodes/gretun/cmd/gretun/commands"
)

func main() {
	if err := commands.Execute(); err != nil {
		os.Exit(1)
	}
}
