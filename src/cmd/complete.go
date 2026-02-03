package cmd

import (
	"fmt"

	"github.com/baitinq/autom8/src/core"
	"github.com/spf13/cobra"
)

var CompleteCmd = &cobra.Command{
	Use:     "complete <task-id>",
	Aliases: []string{"done"},
	Short:   "Mark a task as completed",
	Long: `Mark a task as completed without accepting a worktree.

This is useful when you've implemented the task manually or want to
close the task without merging any worktree implementation.`,
	Example: `  autom8 complete my-task
  autom8 done my-task`,
	Args: cobra.ExactArgs(1),
	RunE: runComplete,
}

func runComplete(cmd *cobra.Command, args []string) error {
	taskID := args[0]

	tasks, err := core.LoadTasks()
	if err != nil {
		return fmt.Errorf("error loading tasks: %w", err)
	}

	taskIndex := core.FindTaskIndex(tasks, taskID)
	if taskIndex == -1 {
		return fmt.Errorf("task '%s' not found\nRun 'autom8 list' to see task names", taskID)
	}

	if tasks[taskIndex].Status == core.TaskStatusCompleted {
		return fmt.Errorf("task '%s' is already completed", taskID)
	}

	tasks[taskIndex].Status = core.TaskStatusCompleted

	if err := core.SaveTasks(tasks); err != nil {
		return fmt.Errorf("error saving tasks: %w", err)
	}

	fmt.Println(SuccessStyle.Render(fmt.Sprintf("Task '%s' marked as completed.", taskID)))
	return nil
}
