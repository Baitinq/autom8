package cmd

import (
	"fmt"
	"strings"

	"github.com/baitinq/autom8/src/core"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

var (
	editNameFlag      string
	editPromptFlag    string
	editCriteriaFlags []string
	editDependsOnFlag string
)

var EditCmd = &cobra.Command{
	Use:   "edit <task-name>",
	Short: "Edit an existing task",
	Long: `Edit an existing task's name, prompt, verification criteria, or dependency.

Without flags, starts an interactive editor to modify the task.
With flags, updates the task directly (non-interactive mode).`,
	Example: `  # Interactive mode
  autom8 edit my-task

  # Non-interactive mode - rename task
  autom8 edit my-task -n new-task-name

  # Non-interactive mode - update prompt only
  autom8 edit my-task -p "New prompt text"

  # Non-interactive mode - replace criteria
  autom8 edit my-task -c "criterion 1" -c "criterion 2"

  # Non-interactive mode - combine flags
  autom8 edit my-task -n renamed-task -p "New prompt" -c "New criterion" -d other-task`,
	Args: cobra.ExactArgs(1),
	RunE: runEdit,
}

func init() {
	EditCmd.Flags().StringVarP(&editNameFlag, "name", "n", "", "Set/replace the task name")
	EditCmd.Flags().StringVarP(&editPromptFlag, "prompt", "p", "", "Set/replace the task prompt")
	EditCmd.Flags().StringArrayVarP(&editCriteriaFlags, "criteria", "c", []string{}, "Set/replace verification criteria (can be specified multiple times)")
	EditCmd.Flags().StringVarP(&editDependsOnFlag, "depends-on", "d", "", "Set/change dependency task ID (use empty string to remove)")
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

	taskIndex := core.FindTaskIndex(tasks, taskID)
	if taskIndex == -1 {
		return fmt.Errorf("task '%s' not found\nRun 'autom8 status' to see task names", taskID)
	}
	task := tasks[taskIndex]

	// Check if any flags were provided for non-interactive mode
	hasFlags := cmd.Flags().Changed("name") || cmd.Flags().Changed("prompt") || cmd.Flags().Changed("criteria") || cmd.Flags().Changed("depends-on")

	var name string
	var prompt string
	var criteria []string
	var dependsOn string

	if hasFlags {
		// Non-interactive mode: apply flag values or keep existing
		if cmd.Flags().Changed("name") {
			name = editNameFlag
			// Validate new name
			if err := core.ValidateTaskName(name); err != nil {
				return err
			}
			if !core.IsTaskNameUnique(tasks, name, taskID) {
				return fmt.Errorf("task name '%s' already exists", name)
			}
		} else {
			name = taskID
		}

		if cmd.Flags().Changed("prompt") {
			prompt = editPromptFlag
		} else {
			prompt = task.Prompt
		}

		if cmd.Flags().Changed("criteria") {
			criteria = editCriteriaFlags
		} else {
			criteria = task.VerificationCriteria
		}

		if cmd.Flags().Changed("depends-on") {
			dependsOn = editDependsOnFlag
		} else {
			dependsOn = task.DependsOn
		}
	} else {
		// Interactive mode
		name = task.ID
		prompt = task.Prompt
		criteriaInput := strings.Join(task.VerificationCriteria, "\n")
		dependsOn = task.DependsOn

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
		if strings.TrimSpace(criteriaInput) != "" {
			for _, line := range strings.Split(criteriaInput, "\n") {
				line = strings.TrimSpace(line)
				if line != "" {
					criteria = append(criteria, line)
				}
			}
		}
	}

	// Validate prompt is not empty
	if strings.TrimSpace(prompt) == "" {
		return fmt.Errorf("prompt cannot be empty")
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
		// Check for circular dependency
		if dependsOn == name {
			return fmt.Errorf("task cannot depend on itself")
		}
	}

	// Update the task
	tasks[taskIndex].ID = name
	tasks[taskIndex].Prompt = prompt
	tasks[taskIndex].VerificationCriteria = criteria
	tasks[taskIndex].DependsOn = dependsOn

	// Promote draft task to pending if now fully defined
	if tasks[taskIndex].Status == core.TaskStatusDraft {
		if strings.TrimSpace(prompt) != "" && len(criteria) > 0 {
			tasks[taskIndex].Status = core.TaskStatusPending
		}
	}

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
