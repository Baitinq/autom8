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

var ChatCmd = &cobra.Command{
	Use:   "chat <worktree-name>",
	Short: "Open an interactive Claude session in a worktree with context",
	Long: `Start an interactive Claude session in a worktree with full task context.

This command gathers context about the worktree including:
  - The original task prompt and verification criteria
  - Commit history since branching from main
  - Current diff from main

This context is passed to Claude via --system-prompt, allowing you to:
  - Ask questions about what was implemented
  - Give instructions to continue or fix the implementation
  - Discuss the approach and make changes interactively`,
	Example: `  autom8 chat my-task-1`,
	Args:    cobra.ExactArgs(1),
	RunE:    runChat,
}

func runChat(cmd *cobra.Command, args []string) error {
	worktreeName := args[0]

	autom8Path, err := core.GetAutom8Dir()
	if err != nil {
		return fmt.Errorf("error getting autom8 dir: %w", err)
	}

	worktreePath := filepath.Join(autom8Path, "worktrees", worktreeName)

	// Check if worktree exists
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		return fmt.Errorf("worktree '%s' not found\nRun 'autom8 status' to see available worktrees", worktreeName)
	}

	// Extract task ID from worktree name: {task-name}-{instance} -> {task-name}
	taskID := worktreeName
	if lastDash := strings.LastIndex(worktreeName, "-"); lastDash > 0 {
		taskID = worktreeName[:lastDash]
	}

	// Load task details
	tasks, err := core.LoadTasks()
	if err != nil {
		return fmt.Errorf("error loading tasks: %w", err)
	}

	var task *core.Task
	for i := range tasks {
		if tasks[i].ID == taskID {
			task = &tasks[i]
			break
		}
	}

	if task == nil {
		return fmt.Errorf("task '%s' not found for worktree '%s'", taskID, worktreeName)
	}

	// Get worktree info for display
	worktreesDir := filepath.Join(autom8Path, "worktrees")
	pids, _ := core.LoadPids()
	info := core.GetWorktreeInfo(worktreesDir, worktreeName, pids)

	// Gather git log since branching from main
	logCmd := exec.Command("git", "-C", worktreePath, "log", "--oneline", "main..HEAD")
	logOutput, _ := logCmd.Output()

	// Gather diff from main
	diffCmd := exec.Command("git", "-C", worktreePath, "diff", "main...HEAD")
	diffOutput, _ := diffCmd.Output()

	// Build system prompt with context
	systemPrompt := buildChatSystemPrompt(task, worktreeName, info.Branch, string(logOutput), string(diffOutput))

	// Display worktree info before starting
	fmt.Println(TitleStyle.Render("Interactive Chat Session"))
	fmt.Println()
	fmt.Printf("  %s %s\n", SubtitleStyle.Render("Worktree:"), HighlightStyle.Render(worktreeName))
	fmt.Printf("  %s %s\n", SubtitleStyle.Render("Branch:"), HighlightStyle.Render(info.Branch))
	fmt.Printf("  %s %s\n", SubtitleStyle.Render("Task ID:"), IDStyle.Render(taskID))
	fmt.Printf("  %s %s\n", SubtitleStyle.Render("Task:"), core.Truncate(task.Prompt, 60))
	if info.CommitsAhead != "0" {
		fmt.Printf("  %s %s commit(s) ahead of main\n", SubtitleStyle.Render("Progress:"), info.CommitsAhead)
	}
	fmt.Println()
	fmt.Println(SubtitleStyle.Render("Starting interactive Claude session with task context..."))
	fmt.Println(SubtitleStyle.Render("Type your questions or instructions. Use Ctrl+C to exit."))
	fmt.Println()

	// Launch interactive Claude session with system prompt
	claudeCmd := exec.Command("claude", "--dangerously-skip-permissions", "--system-prompt", systemPrompt)
	claudeCmd.Dir = worktreePath
	claudeCmd.Stdin = os.Stdin
	claudeCmd.Stdout = os.Stdout
	claudeCmd.Stderr = os.Stderr

	if err := claudeCmd.Run(); err != nil {
		// Exit code from claude is not necessarily an error for us
		if _, ok := err.(*exec.ExitError); !ok {
			return fmt.Errorf("error running claude: %w", err)
		}
	}

	fmt.Println()
	fmt.Println(SuccessStyle.Render("Chat session ended."))
	return nil
}

func buildChatSystemPrompt(task *core.Task, worktreeName, branchName, gitLog, gitDiff string) string {
	var sb strings.Builder

	sb.WriteString("# Context for This Worktree\n\n")
	sb.WriteString("You are assisting with an implementation task in a git worktree. ")
	sb.WriteString("The user wants to either ask questions about the implementation or give you instructions to continue/fix it.\n\n")

	sb.WriteString("## Original Task\n\n")
	sb.WriteString(task.Prompt)
	sb.WriteString("\n\n")

	if len(task.VerificationCriteria) > 0 {
		sb.WriteString("## Verification Criteria\n\n")
		sb.WriteString("The implementation should satisfy these criteria:\n")
		for _, c := range task.VerificationCriteria {
			sb.WriteString(fmt.Sprintf("- %s\n", c))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Current State\n\n")
	sb.WriteString(fmt.Sprintf("- **Worktree:** %s\n", worktreeName))
	sb.WriteString(fmt.Sprintf("- **Branch:** %s\n", branchName))
	sb.WriteString(fmt.Sprintf("- **Task ID:** %s\n\n", task.ID))

	if gitLog != "" {
		sb.WriteString("## Commits Since Main\n\n")
		sb.WriteString("These commits have been made in this worktree:\n\n")
		sb.WriteString("```\n")
		sb.WriteString(gitLog)
		sb.WriteString("```\n\n")
	} else {
		sb.WriteString("## Commits Since Main\n\n")
		sb.WriteString("No commits have been made yet in this worktree.\n\n")
	}

	if gitDiff != "" {
		// Truncate very large diffs to avoid overwhelming the context
		diff := gitDiff
		if len(diff) > 50000 {
			diff = diff[:50000] + "\n... (diff truncated due to size)"
		}
		sb.WriteString("## Current Diff from Main\n\n")
		sb.WriteString("```diff\n")
		sb.WriteString(diff)
		sb.WriteString("```\n\n")
	} else {
		sb.WriteString("## Current Diff from Main\n\n")
		sb.WriteString("No changes from main yet.\n\n")
	}

	sb.WriteString("## Your Role\n\n")
	sb.WriteString("Help the user with this implementation. They may:\n")
	sb.WriteString("- Ask questions about what has been implemented\n")
	sb.WriteString("- Request explanations of the code changes\n")
	sb.WriteString("- Give instructions to continue or fix the implementation\n")
	sb.WriteString("- Ask you to make specific changes\n\n")
	sb.WriteString("You have full access to the codebase in this worktree. Feel free to read files, make edits, and run commands as needed.\n")

	return sb.String()
}
