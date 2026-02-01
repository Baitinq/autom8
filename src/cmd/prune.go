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

var PruneCmd = &cobra.Command{
	Use:     "prune",
	Aliases: []string{"clean"},
	Short:   "Delete all completed tasks",
	Long:  `Remove all tasks with status "completed" from the task list.`,
	RunE:  runPrune,
}

func runPrune(cmd *cobra.Command, args []string) error {
	gitRoot, err := core.GetGitRoot()
	if err != nil {
		return err
	}

	tasks, err := core.LoadTasks()
	if err != nil {
		return fmt.Errorf("error loading tasks: %w", err)
	}

	autom8Path, _ := core.GetAutom8Dir()
	worktreesDir := filepath.Join(autom8Path, "worktrees")

	var remaining []core.Task
	var pruned int
	var worktreesRemoved int

	for _, t := range tasks {
		if t.Status == "completed" {
			pruned++
			// Find and remove worktrees for this task
			if entries, err := os.ReadDir(worktreesDir); err == nil {
				for _, entry := range entries {
					if !entry.IsDir() {
						continue
					}
					worktreeName := entry.Name()
					// Check if worktree belongs to this task ({task-name}-{instance})
					if strings.HasPrefix(worktreeName, t.ID+"-") {
						worktreePath := filepath.Join(worktreesDir, worktreeName)
						// Get branch name before removing
						branchCmd := exec.Command("git", "-C", worktreePath, "branch", "--show-current")
						branchOutput, _ := branchCmd.Output()
						branchName := strings.TrimSpace(string(branchOutput))

						// Remove worktree
						removeCmd := exec.Command("git", "-C", gitRoot, "worktree", "remove", "--force", worktreePath)
						if removeCmd.Run() == nil {
							worktreesRemoved++
							// Delete the branch
							if branchName != "" {
								deleteBranchCmd := exec.Command("git", "-C", gitRoot, "branch", "-D", branchName)
								deleteBranchCmd.Run()
							}
						}
					}
				}
			}
		} else {
			remaining = append(remaining, t)
		}
	}

	if pruned == 0 {
		fmt.Println(SubtitleStyle.Render("No completed tasks to prune."))
		return nil
	}

	if err := core.SaveTasks(remaining); err != nil {
		return fmt.Errorf("error saving tasks: %w", err)
	}

	fmt.Println(SuccessStyle.Render(fmt.Sprintf("Pruned %d completed task(s), removed %d worktree(s).", pruned, worktreesRemoved)))
	return nil
}
