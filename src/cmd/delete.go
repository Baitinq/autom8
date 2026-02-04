package cmd

import (
	"fmt"

	"github.com/baitinq/autom8/src/core"
	"github.com/spf13/cobra"
)

var DeleteCmd = &cobra.Command{
	Use:     "delete <task-name>",
	Aliases: []string{"rm", "remove"},
	Short:   "Delete a task by name",
	Long: `Delete a task from the task list.

Note: Tasks that have other tasks depending on them cannot be deleted
until their dependents are deleted first.`,
	Example: `  autom8 delete my-task`,
	Args:    cobra.ExactArgs(1),
	RunE:    runDelete,
}

func runDelete(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("task name required\nRun 'autom8 list' to see task names")
	}

	taskID := args[0]

	tasks, err := core.LoadTasks()
	if err != nil {
		return fmt.Errorf("error loading tasks: %w", err)
	}

	taskIndex := core.FindTaskIndex(tasks, taskID)
	if taskIndex == -1 {
		return fmt.Errorf("task '%s' not found\nRun 'autom8 list' to see task names", taskID)
	}

	// Check if any other tasks depend on this one
	var dependents []string
	for _, t := range tasks {
		if t.DependsOn == taskID {
			dependents = append(dependents, t.ID)
		}
	}

	if len(dependents) > 0 {
		msg := fmt.Sprintf("cannot delete task '%s' because these tasks depend on it:\n", taskID)
		for _, dep := range dependents {
			msg += fmt.Sprintf("  - %s\n", dep)
		}
		msg += "Delete the dependent tasks first, or use a different approach."
		return fmt.Errorf("%s", msg)
	}

	// Remove the task
	tasks = append(tasks[:taskIndex], tasks[taskIndex+1:]...)

	if err := core.SaveTasks(tasks); err != nil {
		return fmt.Errorf("error saving tasks: %w", err)
	}

	fmt.Println(SuccessStyle.Render(fmt.Sprintf("Task '%s' deleted.", taskID)))
	return nil
}
