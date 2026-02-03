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

var PrCmd = &cobra.Command{
	Use:   "pr <worktree>",
	Short: "Create a GitHub pull request for a worktree",
	Long: `Create a GitHub pull request for a worktree's implementation.

This command requires:
  - The 'gh' CLI must be installed and authenticated

The PR is created as a draft using 'gh pr create --draft'.`,
	Example: `  autom8 pr my-task-1`,
	Args:    cobra.ExactArgs(1),
	RunE:    runPr,
}

func runPr(cmd *cobra.Command, args []string) error {
	// Check if gh CLI is available
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("'gh' CLI is not installed or not in PATH\nInstall it from: https://cli.github.com/")
	}

	worktreeName := args[0]

	// Load tasks for reverse lookup
	tasks, err := core.LoadTasks()
	if err != nil {
		return fmt.Errorf("error loading tasks: %w", err)
	}

	// Build task ID map
	taskIDs := make(map[string]struct{}, len(tasks))
	for _, t := range tasks {
		taskIDs[t.ID] = struct{}{}
	}

	// Reverse-lookup task from worktree name
	taskID, ok := core.TaskIDFromWorktree(worktreeName, taskIDs)
	if !ok {
		return fmt.Errorf("could not determine task from worktree '%s'", worktreeName)
	}

	// Find the task
	taskIndex := core.FindTaskIndex(tasks, taskID)
	if taskIndex == -1 {
		return fmt.Errorf("task '%s' not found", taskID)
	}
	task := tasks[taskIndex]

	// Get worktree path
	worktreesDir, err := core.GetWorktreesDir()
	if err != nil {
		return fmt.Errorf("error getting worktrees dir: %w", err)
	}

	worktreePath := filepath.Join(worktreesDir, worktreeName)

	// Check if worktree exists
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		return fmt.Errorf("worktree '%s' not found", worktreeName)
	}

	// Get worktree info for display
	pids, _ := core.LoadPids()
	info := core.GetWorktreeInfo(worktreesDir, worktreeName, pids)

	// Build prompts for Claude to create a PR
	systemPrompt := buildPrSystemPrompt(&task, worktreeName, info.Branch)
	userPrompt := "Create the draft PR now using `gh pr create --draft`, following the system instructions."

	// Display info before starting
	fmt.Println(TitleStyle.Render("Create Pull Request"))
	fmt.Println()
	fmt.Printf("  %s %s\n", SubtitleStyle.Render("Task:"), IDStyle.Render(taskID))
	fmt.Printf("  %s %s\n", SubtitleStyle.Render("Worktree:"), HighlightStyle.Render(worktreeName))
	fmt.Printf("  %s %s\n", SubtitleStyle.Render("Branch:"), HighlightStyle.Render(info.Branch))
	fmt.Println()
	fmt.Println(SubtitleStyle.Render("Starting Claude session to create PR..."))
	fmt.Println()

	// Launch Claude session with system prompt and initial instruction
	claudeCmd := exec.Command("claude", "--dangerously-skip-permissions", "--system-prompt", systemPrompt, "-p", userPrompt)
	claudeCmd.Dir = worktreePath
	claudeCmd.Stdin = os.Stdin
	claudeCmd.Stdout = os.Stdout
	claudeCmd.Stderr = os.Stderr

	if err := claudeCmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			return fmt.Errorf("error running claude: %w", err)
		}
	}

	fmt.Println()
	fmt.Println(SuccessStyle.Render("PR creation session completed."))
	return nil
}

func buildPrSystemPrompt(task *core.Task, worktreeName, branchName string) string {
	var sb strings.Builder

	sb.WriteString("# Create a Pull Request\n\n")
	sb.WriteString("You need to create a GitHub pull request for this implementation.\n\n")

	sb.WriteString("## Task Description\n\n")
	sb.WriteString(task.Prompt)
	sb.WriteString("\n\n")

	if len(task.VerificationCriteria) > 0 {
		sb.WriteString("## Verification Criteria\n\n")
		sb.WriteString("The implementation satisfies these criteria:\n")
		for _, c := range task.VerificationCriteria {
			sb.WriteString(fmt.Sprintf("- %s\n", c))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Instructions\n\n")
	sb.WriteString("1. First, check what changes are in this branch using `git log` and `git diff`\n")
	sb.WriteString("2. Create a draft pull request using `gh pr create --draft`\n")
	sb.WriteString("3. Generate a clear, concise PR title based on the task description\n")
	sb.WriteString("4. Write a PR description that:\n")
	sb.WriteString("   - Summarizes what was implemented\n")
	sb.WriteString("   - References the task's verification criteria\n")
	sb.WriteString("   - Highlights any key design decisions\n\n")

	sb.WriteString("## Important\n\n")
	sb.WriteString(fmt.Sprintf("- Branch: %s\n", branchName))
	sb.WriteString(fmt.Sprintf("- Worktree: %s\n", worktreeName))
	sb.WriteString("- Create the PR as a draft (--draft flag)\n")
	sb.WriteString("- The PR should be created from this branch to the default branch\n\n")

	sb.WriteString("Run the `gh pr create --draft` command now to create the pull request.\n")

	return sb.String()
}
