package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/baitinq/autom8/src/core"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

var (
	promptFlag    string
	criteriaFlags []string
	dependsOnFlag string
)

var NewCmd = &cobra.Command{
	Use:     "new",
	Aliases: []string{"add", "create"},
	Short:   "Create a new task/prompt",
	Long: `Create a new task with a prompt and optional verification criteria.

Without flags, starts an interactive mode to guide you through task creation.
With flags, creates the task directly (non-interactive mode).`,
	Example: `  # Interactive mode
  autom8 new

  # Non-interactive mode
  autom8 new -p "Add login page" -c "Has email field" -c "Has password field"

  # With dependency
  autom8 new -p "Add logout button" -d task-123456789`,
	RunE: runFeature,
}

func init() {
	NewCmd.Flags().StringVarP(&promptFlag, "prompt", "p", "", "Task prompt (non-interactive mode)")
	NewCmd.Flags().StringArrayVarP(&criteriaFlags, "criteria", "c", []string{}, "Verification criteria (can be specified multiple times)")
	NewCmd.Flags().StringVarP(&dependsOnFlag, "depends-on", "d", "", "Task ID this depends on")
}

func runFeature(cmd *cobra.Command, args []string) error {
	// Check git repo first
	if _, err := core.GetGitRoot(); err != nil {
		return err
	}

	var prompt string
	var criteria []string
	var dependsOn string

	if promptFlag != "" {
		// Non-interactive mode
		prompt = promptFlag
		criteria = criteriaFlags
		dependsOn = dependsOnFlag
	} else {
		// Interactive mode with huh
		var criteriaInput string

		// Load existing tasks for dependency selection
		existingTasks, _ := core.LoadTasks()

		// Build dependency options
		dependsOnOptions := []huh.Option[string]{
			huh.NewOption[string]("None (independent task)", ""),
		}
		for _, t := range existingTasks {
			label := fmt.Sprintf("%s - %s", t.ID, core.Truncate(t.Prompt, 40))
			dependsOnOptions = append(dependsOnOptions, huh.NewOption[string](label, t.ID))
		}

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewText().
					Title("Task Prompt").
					Description("What should the AI implement?").
					Placeholder("Add a login page with email and password fields...").
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
					Placeholder("Has email field\nHas password field\nValidates input").
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

		err := form.Run()
		if err != nil {
			if err == huh.ErrUserAborted {
				fmt.Println("\nAborted.")
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

	if strings.TrimSpace(prompt) == "" {
		return fmt.Errorf("no prompt provided")
	}

	tasks, err := core.LoadTasks()
	if err != nil {
		return fmt.Errorf("error loading tasks: %w", err)
	}

	// Validate dependency exists if specified
	if dependsOn != "" {
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
	}

	task := core.Task{
		ID:                   fmt.Sprintf("task-%d", time.Now().UnixNano()),
		Prompt:               prompt,
		VerificationCriteria: criteria,
		DependsOn:            dependsOn,
		CreatedAt:            time.Now(),
		Status:               "pending",
	}

	tasks = append(tasks, task)

	if err := core.SaveTasks(tasks); err != nil {
		return fmt.Errorf("error saving task: %w", err)
	}

	fmt.Println()
	fmt.Println(SuccessStyle.Render("Task created successfully!"))
	fmt.Printf("  %s %s\n", SubtitleStyle.Render("ID:"), IDStyle.Render(task.ID))
	return nil
}
