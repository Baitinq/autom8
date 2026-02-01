package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/baitinq/autom8/src/core"
	"github.com/spf13/cobra"
)

var AcceptCmd = &cobra.Command{
	Use:     "accept <worktree-name>",
	Aliases: []string{"merge"},
	Short:   "Merge a worktree branch into current branch",
	Long: `Accept and merge a completed implementation from a worktree.

This command will:
  1. Auto-commit any uncommitted changes in the worktree
  2. Merge the worktree's branch into your current branch

The worktree and branch are preserved. Use 'autom8 prune' to clean them up.`,
	Example: `  autom8 accept my-task-1`,
	Args:    cobra.ExactArgs(1),
	RunE:    runAccept,
}

func runAccept(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("worktree name required\nRun 'autom8 status' to see available worktrees")
	}

	worktreeName := args[0]

	gitRoot, err := core.GetGitRoot()
	if err != nil {
		return fmt.Errorf("error getting git root: %w", err)
	}

	autom8Path, err := core.GetAutom8Dir()
	if err != nil {
		return fmt.Errorf("error getting autom8 dir: %w", err)
	}

	worktreePath := filepath.Join(autom8Path, "worktrees", worktreeName)

	// Check if worktree exists
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		return fmt.Errorf("worktree '%s' not found\nRun 'autom8 status' to see available worktrees", worktreeName)
	}

	// Get the branch name from the worktree
	branchCmd := exec.Command("git", "-C", worktreePath, "branch", "--show-current")
	branchOutput, err := branchCmd.Output()
	if err != nil {
		return fmt.Errorf("error getting branch name: %w", err)
	}
	branchName := strings.TrimSpace(string(branchOutput))

	if branchName == "" {
		return fmt.Errorf("could not determine branch name for worktree")
	}

	// Check for uncommitted changes in the worktree
	statusCmd := exec.Command("git", "-C", worktreePath, "status", "--porcelain")
	statusOutput, err := statusCmd.Output()
	if err != nil {
		return fmt.Errorf("error checking worktree status: %w", err)
	}

	if len(strings.TrimSpace(string(statusOutput))) > 0 {
		fmt.Println(SubtitleStyle.Render("Found uncommitted changes, auto-committing..."))

		// Stage all changes
		addCmd := exec.Command("git", "-C", worktreePath, "add", "-A")
		if addOutput, err := addCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("error staging changes: %w\n%s", err, string(addOutput))
		}

		// Commit with auto-commit message
		commitCmd := exec.Command("git", "-C", worktreePath, "commit", "-m", "autom8: auto-commit uncommitted changes")
		if commitOutput, err := commitCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("error committing changes: %w\n%s", err, string(commitOutput))
		}
		fmt.Println(SuccessStyle.Render("Auto-committed successfully."))
	}

	fmt.Printf("Merging branch '%s' into current branch...\n", HighlightStyle.Render(branchName))

	// Merge the branch into the current branch
	mergeCmd := exec.Command("git", "-C", gitRoot, "merge", branchName, "-m", fmt.Sprintf("Merge %s (autom8 accept)", branchName))
	mergeOutput, err := mergeCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error merging branch: %w\n%s\nResolve conflicts manually, then run 'autom8 accept' again to clean up", err, string(mergeOutput))
	}
	fmt.Printf("%s", string(mergeOutput))

	// Mark the task as completed
	// Worktree name format: {task-name}-{instance} (e.g., my-task-1)
	// Extract task ID by removing the last -{instance} suffix
	taskID := worktreeName
	if lastDash := strings.LastIndex(worktreeName, "-"); lastDash > 0 {
		taskID = worktreeName[:lastDash]
	}

	tasks, err := core.LoadTasks()
	if err != nil {
		fmt.Printf("%s could not load tasks to update status: %v\n", ErrorStyle.Render("Warning:"), err)
	} else {
		for i, t := range tasks {
			if t.ID == taskID {
				tasks[i].Status = "completed"
				if err := core.SaveTasks(tasks); err != nil {
					fmt.Printf("%s could not save task status: %v\n", ErrorStyle.Render("Warning:"), err)
				} else {
					fmt.Printf("Marked task '%s' as completed.\n", taskID)
				}
				break
			}
		}
	}

	fmt.Println()
	fmt.Println(SuccessStyle.Render(fmt.Sprintf("Successfully accepted worktree '%s'", worktreeName)))
	return nil
}
