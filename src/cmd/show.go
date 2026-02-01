package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/baitinq/autom8/src/core"
	"github.com/spf13/cobra"
)

var ShowCmd = &cobra.Command{
	Use:     "show <worktree-name>",
	Aliases: []string{"diff"},
	Short:   "Show the diff between main and a worktree (PR-style)",
	Long: `Display the changes in a worktree compared to the main branch.

This shows the diff in a PR-style format, making it easy to review what
changes an implementation has made.`,
	Example: `  autom8 show task-123456789-1`,
	Args:    cobra.ExactArgs(1),
	RunE:    runShow,
}

func runShow(cmd *cobra.Command, args []string) error {
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

	// Print header info directly to stdout
	fmt.Println(TitleStyle.Render(fmt.Sprintf("Diff: main...%s", info.Branch)))
	fmt.Println()
	fmt.Printf("  %s %s\n", SubtitleStyle.Render("Worktree:"), HighlightStyle.Render(worktreeName))
	fmt.Printf("  %s %s\n", SubtitleStyle.Render("Branch:"), HighlightStyle.Render(info.Branch))
	fmt.Printf("  %s %s commit(s) ahead of main\n", SubtitleStyle.Render("Commits:"), info.CommitsAhead)
	fmt.Println()

	// Get the diff between main and the worktree branch
	diffCmd := exec.Command("git", "-C", worktreePath, "diff", "main...HEAD", "--stat")
	statOutput, _ := diffCmd.Output()

	if len(statOutput) > 0 {
		fmt.Println(SubtitleStyle.Render("Files changed:"))
		fmt.Println(string(statOutput))
	}

	// Get the full diff
	fullDiffCmd := exec.Command("git", "-C", worktreePath, "diff", "main...HEAD")
	fullDiffOutput, err := fullDiffCmd.Output()
	if err != nil {
		return fmt.Errorf("error getting diff: %w", err)
	}

	if len(fullDiffOutput) == 0 {
		fmt.Println(SubtitleStyle.Render("No changes from main."))
		return nil
	}

	fmt.Println(SubtitleStyle.Render("Diff:"))
	fmt.Println()

	// Pipe the full diff through less for scrollable viewing
	// Fall back to direct print if less is unavailable
	if err := pipeToLess(fullDiffOutput); err != nil {
		// Fallback: print directly to stdout
		fmt.Println(string(fullDiffOutput))
	}

	return nil
}

// pipeToLess pipes the given content through the less pager for scrollable viewing.
// Returns an error if less is unavailable or fails to run.
func pipeToLess(content []byte) error {
	// Check if less is available
	lessPath, err := exec.LookPath("less")
	if err != nil {
		return fmt.Errorf("less not found: %w", err)
	}

	// Create the less command with options for color support
	lessCmd := exec.Command(lessPath, "-R")
	lessCmd.Stdout = os.Stdout
	lessCmd.Stderr = os.Stderr

	// Get stdin pipe to write content
	stdin, err := lessCmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	// Start less
	if err := lessCmd.Start(); err != nil {
		return fmt.Errorf("failed to start less: %w", err)
	}

	// Write content to less stdin
	stdin.Write(content)
	stdin.Close()

	// Wait for less to finish (user quits with 'q')
	if err := lessCmd.Wait(); err != nil {
		// Ignore exit errors from less (e.g., user pressing 'q')
		if _, ok := err.(*exec.ExitError); !ok {
			return fmt.Errorf("less failed: %w", err)
		}
	}

	return nil
}
