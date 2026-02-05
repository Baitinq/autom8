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
	investigateNameFlag      string
	investigatePromptFlag    string
	investigateCriteriaFlags []string
)

var InvestigateCmd = &cobra.Command{
	Use:   "investigate",
	Short: "Create a new investigation task",
	Long: `Create an investigation task for understanding, debugging, or researching code.

Investigation tasks differ from regular implementation tasks:
  - The goal is understanding, not code changes
  - Agents use investigation-focused prompts
  - Output is written to .autom8/investigations/<task-id>.md

Without flags, starts an interactive mode to guide you through task creation.
With flags, creates the task directly (non-interactive mode).`,
	Example: `  # Interactive mode
  autom8 investigate

  # Non-interactive mode
  autom8 investigate -n debug-flaky-test -p "Why is TestFoo flaky?"

  # With success criteria
  autom8 investigate -n auth-flow -p "How does auth work?" -c "Document the flow" -c "List all auth endpoints"`,
	RunE: runInvestigate,
}

func init() {
	InvestigateCmd.Flags().StringVarP(&investigateNameFlag, "name", "n", "", "Task name (unique identifier)")
	InvestigateCmd.Flags().StringVarP(&investigatePromptFlag, "prompt", "p", "", "Investigation question/goal (non-interactive mode)")
	InvestigateCmd.Flags().StringArrayVarP(&investigateCriteriaFlags, "criteria", "c", []string{}, "Success criteria (can be specified multiple times)")
}

func runInvestigate(cmd *cobra.Command, args []string) error {
	if _, err := core.GetGitRoot(); err != nil {
		return err
	}

	var name string
	var prompt string
	var criteria []string

	existingTasks, err := core.LoadTasks()
	if err != nil {
		return fmt.Errorf("error loading tasks: %w", err)
	}

	if investigateNameFlag != "" || investigatePromptFlag != "" {
		// Non-interactive mode
		if investigateNameFlag == "" {
			return fmt.Errorf("task name is required (use -n flag)")
		}
		if investigatePromptFlag == "" {
			return fmt.Errorf("investigation prompt is required (use -p flag)")
		}
		name = investigateNameFlag
		prompt = investigatePromptFlag
		criteria = investigateCriteriaFlags

		if err := core.ValidateTaskName(name); err != nil {
			return err
		}
		if !core.IsTaskNameUnique(existingTasks, name, "") {
			return fmt.Errorf("task name '%s' already exists", name)
		}
	} else {
		// Interactive mode
		var criteriaInput string

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Investigation Name").
					Description("Unique identifier (alphanumeric, dashes, underscores, max 50 chars)").
					Placeholder("debug-flaky-test").
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
					Title("Investigation Question").
					Description("What do you want to understand or investigate?").
					Placeholder("Why is TestFoo flaky? What conditions cause it to fail?").
					Value(&prompt).
					Validate(func(s string) error {
						if strings.TrimSpace(s) == "" {
							return fmt.Errorf("investigation question cannot be empty")
						}
						return nil
					}),
			),
			huh.NewGroup(
				huh.NewText().
					Title("Success Criteria").
					Description("What does a successful investigation look like? (one per line, optional)").
					Placeholder("Identified root cause\nDocumented reproduction steps\nProposed fix approach").
					Value(&criteriaInput),
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
		return fmt.Errorf("no investigation question provided")
	}

	task := core.Task{
		ID:                   name,
		Type:                 core.TaskTypeInvestigation,
		Prompt:               prompt,
		VerificationCriteria: criteria,
		CreatedAt:            time.Now(),
		Status:               core.TaskStatusPending,
	}

	tasks := append(existingTasks, task)

	if err := core.SaveTasks(tasks); err != nil {
		return fmt.Errorf("error saving task: %w", err)
	}

	fmt.Println()
	fmt.Println(SuccessStyle.Render("Investigation task created!"))
	fmt.Printf("  %s %s\n", SubtitleStyle.Render("ID:"), IDStyle.Render(task.ID))
	fmt.Printf("  %s %s\n", SubtitleStyle.Render("Type:"), HighlightStyle.Render("investigation"))
	return nil
}
