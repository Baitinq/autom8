package main

import (
	"os"

	"github.com/baitinq/autom8/src/cmd"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "autom8",
	Short: "Automate AI agent workflows",
	Long: `autom8 is a CLI tool that orchestrates AI-driven development workflows.

It enables you to:
  - Define implementation tasks with verification criteria
  - Manage task dependencies
  - Run multiple Claude AI agents in parallel
  - Isolate each agent's work in separate git worktrees`,
	SilenceUsage:      true,
	CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
}

func init() {
	rootCmd.AddCommand(cmd.NewCmd)
	rootCmd.AddCommand(cmd.ImplementCmd)
	rootCmd.AddCommand(cmd.StatusCmd)
	rootCmd.AddCommand(cmd.AcceptCmd)
	rootCmd.AddCommand(cmd.DeleteCmd)
	rootCmd.AddCommand(cmd.InspectCmd)
	rootCmd.AddCommand(cmd.DescribeCmd)
	rootCmd.AddCommand(cmd.EditCmd)
	rootCmd.AddCommand(cmd.PruneCmd)
	rootCmd.AddCommand(cmd.ConvergeCmd)
	rootCmd.AddCommand(cmd.ShowCmd)
	rootCmd.AddCommand(cmd.ChatCmd)
	rootCmd.AddCommand(cmd.WorkerCmd)
	rootCmd.AddCommand(cmd.LogsCmd)
	rootCmd.AddCommand(cmd.CompleteCmd)
	rootCmd.AddCommand(cmd.PrCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
