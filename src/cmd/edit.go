package cmd

import (
	"fmt"
	"strings"

	"github.com/baitinq/autom8/src/core"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

var EditCmd = &cobra.Command{
	Use:   "edit <task-name>",
	Short: "Edit an existing task",
	Long: `Edit an existing task's name, prompt, verification criteria, or dependency.

Starts an interactive editor to modify the task. All fields are optional -
press Enter to keep the current value.`,
	Example: `  autom8 edit my-task`,
	Args:    cobra.ExactArgs(1),
	RunE:    runEdit,
}

func runEdit(cmd *cobra.Command, args []string) error {
	taskID := args[0]

	if _, err := core.GetGitRoot(); err != nil {
		return err
	}

	tasks, err := core.LoadTasks()
	if err != nil {
		return fmt.Errorf("error loading tasks: %w", err)
	}

	// Find the task
	var taskIndex int = -1
	var task *core.Task
	for i := range tasks {
		if tasks[i].ID == taskID {
			taskIndex = i
			task = &tasks[i]
			break
		}
	}

	if task == nil {
		return fmt.Errorf("task '%s' not found\nRun 'autom8 status' to see task names", taskID)
	}

	// Prepare current values for editing
	name := task.ID
	prompt := task.Prompt
	criteriaInput := strings.Join(task.VerificationCriteria, "\n")
	dependsOn := task.DependsOn

	// Build dependency options (exclude current task to prevent self-reference)
	dependsOnOptions := []huh.Option[string]{
		huh.NewOption[string]("None (independent task)", ""),
	}
	for _, t := range tasks {
		if t.ID != taskID { // Can't depend on itself
			label := fmt.Sprintf("%s - %s", t.ID, core.Truncate(t.Prompt, 40))
			dependsOnOptions = append(dependsOnOptions, huh.NewOption[string](label, t.ID))
		}
	}

	// Interactive editing with huh
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Task Name").
				Description("Unique identifier (alphanumeric, dashes, underscores, max 50 chars)").
				Value(&name).
				Validate(func(s string) error {
					if err := core.ValidateTaskName(s); err != nil {
						return err
					}
					// Allow keeping the same name, but check uniqueness for new names
					if s != taskID && !core.IsTaskNameUnique(tasks, s, taskID) {
						return fmt.Errorf("task name '%s' already exists", s)
					}
					return nil
				}),
		),
		huh.NewGroup(
			huh.NewText().
				Title("Task Prompt").
				Description("What should the AI implement?").
				Value(&prompt).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("prompt cannot be empty")
					}
					return nil
				}),
		),
		huh.NewGroup(
			huh.NewText().
				Title("Verification Criteria").
				Description("How should success be verified? (one per line, optional)").
				Value(&criteriaInput),
		),
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Depends On").
				Description("Select a task this depends on (optional)").
				Options(dependsOnOptions...).
				Value(&dependsOn),
		),
	).WithTheme(huh.ThemeDracula())

	err = form.Run()
	if err != nil {
		if err == huh.ErrUserAborted {
			fmt.Println("\nAborted. No changes made.")
			return nil
		}
		return err
	}

	// Parse criteria from multiline input
	var criteria []string
	if strings.TrimSpace(criteriaInput) != "" {
		for _, line := range strings.Split(criteriaInput, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				criteria = append(criteria, line)
			}
		}
	}

	// Validate dependency exists if specified
	if dependsOn != "" && dependsOn != task.DependsOn {
		found := false
		for _, t := range tasks {
			if t.ID == dependsOn {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("dependency task '%s' not found", dependsOn)
		}
		// Check for circular dependency (use new name if changed)
		if dependsOn == name {
			return fmt.Errorf("task cannot depend on itself")
		}
	}

	// Update the task
	tasks[taskIndex].ID = name
	tasks[taskIndex].Prompt = prompt
	tasks[taskIndex].VerificationCriteria = criteria
	tasks[taskIndex].DependsOn = dependsOn

	// Update any tasks that depend on the old name
	if name != taskID {
		for i := range tasks {
			if tasks[i].DependsOn == taskID {
				tasks[i].DependsOn = name
			}
		}
	}

	if err := core.SaveTasks(tasks); err != nil {
		return fmt.Errorf("error saving task: %w", err)
	}

	fmt.Println()
	fmt.Println(SuccessStyle.Render("Task updated successfully!"))
	fmt.Printf("  %s %s\n", SubtitleStyle.Render("Name:"), IDStyle.Render(name))
	return nil
}
