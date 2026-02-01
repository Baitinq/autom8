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
	Aliases: []string{"clean", "purge"},
	Short:   "Delete all completed tasks",
	Long:    `Remove all tasks with status "completed" from the task list.`,
	RunE:    runPrune,
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

	taskIDs := make(map[string]struct{}, len(tasks))
	for _, t := range tasks {
		taskIDs[t.ID] = struct{}{}
	}

	worktreesDir, err := core.GetWorktreesDir()
	if err != nil {
		return err
	}

	taskMap := make(map[string]core.Task, len(tasks))
	for _, t := range tasks {
		taskMap[t.ID] = t
	}

	protected := make(map[string]bool, len(tasks))
	for _, t := range tasks {
		if t.Status == core.TaskStatusCompleted {
			continue
		}
		currentID := t.ID
		for currentID != "" {
			if protected[currentID] {
				break
			}
			protected[currentID] = true
			current, ok := taskMap[currentID]
			if !ok || current.DependsOn == "" {
				break
			}
			currentID = current.DependsOn
		}
	}

	var remaining []core.Task
	var pruned int
	var worktreesRemoved int
	var skippedDependents int

	for _, t := range tasks {
		if t.Status == core.TaskStatusCompleted {
			if protected[t.ID] {
				// Keep task because it's needed by active dependent tasks.
				remaining = append(remaining, t)
				skippedDependents++
				continue
			}
			pruned++
			// Find and remove worktrees for this task
			if entries, err := os.ReadDir(worktreesDir); err == nil {
				for _, entry := range entries {
					if !entry.IsDir() {
						continue
					}
					worktreeName := entry.Name()
					worktreeTaskID, ok := core.TaskIDFromWorktree(worktreeName, taskIDs)
					if !ok || worktreeTaskID != t.ID {
						continue
					}
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
		} else {
			remaining = append(remaining, t)
		}
	}

	if pruned == 0 && skippedDependents == 0 {
		fmt.Println(SubtitleStyle.Render("No completed tasks to prune."))
		return nil
	}

	if pruned == 0 && skippedDependents > 0 {
		fmt.Println(SubtitleStyle.Render(fmt.Sprintf("Skipped %d completed task(s) needed by active dependents.", skippedDependents)))
		return nil
	}

	if err := core.SaveTasks(remaining); err != nil {
		return fmt.Errorf("error saving tasks: %w", err)
	}

	msg := fmt.Sprintf("Pruned %d completed task(s), removed %d worktree(s).", pruned, worktreesRemoved)
	if skippedDependents > 0 {
		msg += fmt.Sprintf(" Skipped %d task(s) needed by active dependents.", skippedDependents)
	}
	fmt.Println(SuccessStyle.Render(msg))
	return nil
}
