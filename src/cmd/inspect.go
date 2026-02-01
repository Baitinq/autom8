package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/baitinq/autom8/src/core"
	"github.com/spf13/cobra"
)

var InspectCmd = &cobra.Command{
	Use:   "inspect <worktree-name>",
	Short: "Enter a worktree directory for inspection",
	Long: `Open a new shell in the specified worktree directory.

This allows you to inspect the implementation, run tests, or make manual changes.
To return to your original directory, simply exit the shell (Ctrl+D or 'exit').`,
	Example: `  autom8 inspect task-123456789-1`,
	Args:    cobra.ExactArgs(1),
	RunE:    runInspect,
}

func runInspect(cmd *cobra.Command, args []string) error {
	worktreeName := args[0]

	autom8Path, err := core.GetAutom8Dir()
	if err != nil {
		return fmt.Errorf("error getting autom8 dir: %w", err)
	}

	worktreePath := filepath.Join(autom8Path, "worktrees", worktreeName)

	// Check if worktree exists
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		return fmt.Errorf("worktree '%s' not found\nRun 'autom8 status' to see available worktrees", worktreeName)
	}

	// Get worktree info for display
	worktreesDir := filepath.Join(autom8Path, "worktrees")
	pids, _ := core.LoadPids()
	info := core.GetWorktreeInfo(worktreesDir, worktreeName, pids)

	fmt.Println(TitleStyle.Render("Inspecting Worktree"))
	fmt.Println()
	fmt.Printf("  %s %s\n", SubtitleStyle.Render("Worktree:"), HighlightStyle.Render(worktreeName))
	fmt.Printf("  %s %s\n", SubtitleStyle.Render("Branch:"), HighlightStyle.Render(info.Branch))
	fmt.Printf("  %s %s\n", SubtitleStyle.Render("Path:"), worktreePath)
	fmt.Println()
	fmt.Println(SubtitleStyle.Render("Starting a new shell in the worktree directory..."))
	fmt.Println(SubtitleStyle.Render("Type 'exit' or press Ctrl+D to return."))
	fmt.Println()

	// Determine which shell to use
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

	// Start an interactive shell in the worktree directory
	shellCmd := exec.Command(shell)
	shellCmd.Dir = worktreePath
	shellCmd.Stdin = os.Stdin
	shellCmd.Stdout = os.Stdout
	shellCmd.Stderr = os.Stderr

	// Set a custom prompt to remind the user they're in an autom8 worktree
	env := os.Environ()
	env = append(env, fmt.Sprintf("AUTOM8_WORKTREE=%s", worktreeName))
	shellCmd.Env = env

	if err := shellCmd.Run(); err != nil {
		// Exit code from shell is not an error for us
		if _, ok := err.(*exec.ExitError); !ok {
			return fmt.Errorf("error running shell: %w", err)
		}
	}

	fmt.Println()
	fmt.Println(SuccessStyle.Render("Exited worktree inspection."))
	return nil
}
