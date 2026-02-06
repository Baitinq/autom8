package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is set by main.VERSION during init.
var Version = "dev"

var VersionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the autom8 version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(Version)
	},
}
