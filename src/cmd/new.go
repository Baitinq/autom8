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
	nameFlag      string
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
  autom8 new -n login-page -p "Add login page" -c "Has email field" -c "Has password field"

  # With dependency
  autom8 new -n logout-button -p "Add logout button" -d login-page`,
	RunE: runFeature,
}

func init() {
	NewCmd.Flags().StringVarP(&nameFlag, "name", "n", "", "Task name (unique identifier)")
	NewCmd.Flags().StringVarP(&promptFlag, "prompt", "p", "", "Task prompt (non-interactive mode)")
	NewCmd.Flags().StringArrayVarP(&criteriaFlags, "criteria", "c", []string{}, "Verification criteria (can be specified multiple times)")
	NewCmd.Flags().StringVarP(&dependsOnFlag, "depends-on", "d", "", "Task ID this depends on")
}

func runFeature(cmd *cobra.Command, args []string) error {
	// Check git repo first
	if _, err := core.GetGitRoot(); err != nil {
		return err
	}

	var name string
	var prompt string
	var criteria []string
	var dependsOn string

	// Load existing tasks (needed for both modes)
	existingTasks, err := core.LoadTasks()
	if err != nil {
		return fmt.Errorf("error loading tasks: %w", err)
	}

	if nameFlag != "" || promptFlag != "" {
		// Non-interactive mode (requires both name and prompt)
		if nameFlag == "" {
			return fmt.Errorf("task name is required (use -n flag)")
		}
		if promptFlag == "" {
			return fmt.Errorf("task prompt is required (use -p flag)")
		}
		name = nameFlag
		prompt = promptFlag
		criteria = criteriaFlags
		dependsOn = dependsOnFlag

		// Validate name
		if err := core.ValidateTaskName(name); err != nil {
			return err
		}
		if !core.IsTaskNameUnique(existingTasks, name, "") {
			return fmt.Errorf("task name '%s' already exists", name)
		}
	} else {
		// Interactive mode with huh
		var criteriaInput string

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
				huh.NewInput().
					Title("Task Name").
					Description("Unique identifier (alphanumeric, dashes, underscores, max 50 chars)").
					Placeholder("my-task-name").
					Value(&name).
					Validate(func(s string) error {
						if err := core.ValidateTaskName(s); err != nil {
							return err
						}
						if !core.IsTaskNameUnique(existingTasks, s, "") {
							return fmt.Errorf("task name '%s' already exists", s)
						}
						return nil
					}),
			),
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

	// Validate dependency exists if specified
	if dependsOn != "" {
		found := false
		for _, t := range existingTasks {
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
		ID:                   name,
		Prompt:               prompt,
		VerificationCriteria: criteria,
		DependsOn:            dependsOn,
		CreatedAt:            time.Now(),
		Status:               "pending",
	}

	tasks := append(existingTasks, task)

	if err := core.SaveTasks(tasks); err != nil {
		return fmt.Errorf("error saving task: %w", err)
	}

	fmt.Println()
	fmt.Println(SuccessStyle.Render("Task created successfully!"))
	fmt.Printf("  %s %s\n", SubtitleStyle.Render("ID:"), IDStyle.Render(task.ID))
	return nil
}
