package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/baitinq/autom8/src/core"
	"github.com/spf13/cobra"
)

var mergeFlag bool

var ConvergeCmd = &cobra.Command{
	Use:   "converge [task-name]",
	Short: "Use AI to pick the best implementation from multiple worktrees",
	Long: `Analyze all worktrees for a task and determine which implementation is best.

An AI agent will inspect the diffs and code from each worktree, comparing them
against the original task prompt and verification criteria to pick a winner.

If no task name is provided, all tasks with multiple worktrees will be evaluated.`,
	Example: `  # Converge all tasks with multiple worktrees
  autom8 converge

  # Converge a specific task
  autom8 converge my-task

  # Converge and auto-merge the winner
  autom8 converge --merge
  autom8 converge my-task --merge`,
	Args: cobra.MaximumNArgs(1),
	RunE: runConverge,
}

func init() {
	ConvergeCmd.Flags().BoolVarP(&mergeFlag, "merge", "m", false, "Auto-merge the winning implementation")
}

func runConverge(cmd *cobra.Command, args []string) error {
	gitRoot, err := core.GetGitRoot()
	if err != nil {
		return err
	}

	tasks, err := core.LoadTasks()
	if err != nil {
		return fmt.Errorf("error loading tasks: %w", err)
	}

	if len(tasks) == 0 {
		fmt.Println(SubtitleStyle.Render("No tasks found."))
		return nil
	}

	// Check if a specific task ID was provided
	var targetTaskID string
	if len(args) > 0 {
		targetTaskID = args[0]
	}

	// Build task ID set for worktree matching
	taskIDs := make(map[string]struct{})
	for _, t := range tasks {
		taskIDs[t.ID] = struct{}{}
	}

	// Get worktrees directory
	autom8Path, err := core.GetAutom8Dir()
	if err != nil {
		return err
	}
	worktreesDir := filepath.Join(autom8Path, "worktrees")
	pids, _ := core.LoadPids()

	// Build map of task ID -> worktrees
	worktreesByTask := core.ListWorktreesByTask(worktreesDir, taskIDs, pids)

	// Filter tasks to converge
	var tasksToConverge []core.Task
	for _, task := range tasks {
		if targetTaskID != "" {
			if task.ID == targetTaskID {
				tasksToConverge = append(tasksToConverge, task)
				break
			}
		} else {
			// Only converge tasks with multiple worktrees
			if len(worktreesByTask[task.ID]) > 1 {
				tasksToConverge = append(tasksToConverge, task)
			}
		}
	}

	if targetTaskID != "" && len(tasksToConverge) == 0 {
		return fmt.Errorf("task '%s' not found", targetTaskID)
	}

	if len(tasksToConverge) == 0 {
		fmt.Println(SubtitleStyle.Render("No tasks with multiple worktrees to converge."))
		return nil
	}

	fmt.Println(TitleStyle.Render("Converging Implementations"))
	fmt.Println()

	// Process each task
	for _, task := range tasksToConverge {
		worktrees := worktreesByTask[task.ID]

		if len(worktrees) == 0 {
			fmt.Printf("  %s %s (no worktrees)\n", SubtitleStyle.Render("[skip]"), task.ID)
			continue
		}

		if len(worktrees) == 1 {
			fmt.Printf("  %s %s (only one worktree, nothing to compare)\n", SubtitleStyle.Render("[skip]"), task.ID)
			continue
		}

		// Check if any worktrees are still running
		anyRunning := false
		for _, wt := range worktrees {
			if wt.IsRunning {
				anyRunning = true
				break
			}
		}
		if anyRunning {
			fmt.Printf("  %s %s (agents still running)\n", StatusInProgressStyle.Render("[wait]"), task.ID)
			continue
		}

		fmt.Printf("  %s %s\n", HighlightStyle.Render("[analyzing]"), core.Truncate(task.Prompt, 50))
		fmt.Printf("    %s %s\n", SubtitleStyle.Render("ID:"), IDStyle.Render(task.ID))
		fmt.Printf("    %s %d worktrees\n", SubtitleStyle.Render("Comparing:"), len(worktrees))

		// Build the converge prompt
		convergePrompt := buildConvergePrompt(task, worktrees, gitRoot)

		// Run claude to analyze
		claudeCmd := exec.Command("claude", "-p", convergePrompt, "--output-format", "json")
		claudeCmd.Dir = gitRoot

		output, err := claudeCmd.Output()
		if err != nil {
			fmt.Printf("    %s failed to run AI analysis: %v\n", ErrorStyle.Render("[error]"), err)
			continue
		}

		// Parse the response to extract the winner
		winner := parseConvergeResponse(string(output), worktrees)
		if winner == "" {
			fmt.Printf("    %s could not determine a winner\n", ErrorStyle.Render("[error]"))
			// Print the raw output for debugging
			fmt.Printf("    %s\n", SubtitleStyle.Render("AI response:"))
			fmt.Printf("    %s\n", string(output))
			continue
		}

		fmt.Printf("    %s %s\n", SuccessStyle.Render("[winner]"), HighlightStyle.Render(winner))

		// Update task with winner
		for i, t := range tasks {
			if t.ID == task.ID {
				tasks[i].Winner = winner
				break
			}
		}

		// Auto-merge if flag is set
		if mergeFlag {
			fmt.Printf("    %s\n", SubtitleStyle.Render("Auto-merging winner..."))
			// Simulate calling accept
			if err := doAccept(winner, gitRoot, autom8Path, tasks); err != nil {
				fmt.Printf("    %s merge failed: %v\n", ErrorStyle.Render("[error]"), err)
			} else {
				fmt.Printf("    %s merged successfully\n", SuccessStyle.Render("[merged]"))
			}
		}

		fmt.Println()
	}

	// Save tasks with winner info
	if err := core.SaveTasks(tasks); err != nil {
		return fmt.Errorf("error saving tasks: %w", err)
	}

	fmt.Println(SuccessStyle.Render("Convergence complete!"))
	if !mergeFlag {
		fmt.Println(SubtitleStyle.Render("Use 'autom8 accept <worktree>' to merge the winner, or 'autom8 converge --merge' to auto-merge."))
	}
	return nil
}

func buildConvergePrompt(task core.Task, worktrees []core.WorktreeInfo, gitRoot string) string {
	var sb strings.Builder

	sb.WriteString("You are evaluating multiple implementations of the same task to determine which is best.\n\n")

	sb.WriteString("## Task\n\n")
	sb.WriteString(task.Prompt)
	sb.WriteString("\n\n")

	if len(task.VerificationCriteria) > 0 {
		sb.WriteString("## Verification Criteria\n\n")
		for _, c := range task.VerificationCriteria {
			sb.WriteString(fmt.Sprintf("- %s\n", c))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Implementations\n\n")
	sb.WriteString("Below are the diffs for each implementation worktree:\n\n")

	for _, wt := range worktrees {
		sb.WriteString(fmt.Sprintf("### Worktree: %s\n\n", wt.Name))

		// Get the diff for this worktree
		diffCmd := exec.Command("git", "-C", wt.Path, "diff", "main...HEAD")
		diffOutput, err := diffCmd.Output()
		if err != nil {
			sb.WriteString("(could not get diff)\n\n")
		} else if len(diffOutput) == 0 {
			sb.WriteString("(no changes from main)\n\n")
		} else {
			// Truncate very large diffs
			diff := string(diffOutput)
			if len(diff) > 50000 {
				diff = diff[:50000] + "\n... (truncated)"
			}
			sb.WriteString("```diff\n")
			sb.WriteString(diff)
			sb.WriteString("\n```\n\n")
		}
	}

	sb.WriteString("## Your Task\n\n")
	sb.WriteString("Analyze each implementation and determine which one best satisfies the task requirements and verification criteria.\n\n")
	sb.WriteString("Consider:\n")
	sb.WriteString("- Correctness: Does the implementation actually solve the task?\n")
	sb.WriteString("- Completeness: Are all verification criteria met?\n")
	sb.WriteString("- Code quality: Is the code clean, readable, and maintainable?\n")
	sb.WriteString("- Simplicity: Is the solution appropriately simple without over-engineering?\n\n")
	sb.WriteString("IMPORTANT: Your response MUST include the exact worktree name of the winner in this format:\n")
	sb.WriteString("WINNER: <worktree-name>\n\n")
	sb.WriteString("For example: WINNER: my-task-1\n\n")
	sb.WriteString("Explain your reasoning before declaring the winner.\n")

	return sb.String()
}

func parseConvergeResponse(response string, worktrees []core.WorktreeInfo) string {
	// Try to parse JSON response first
	var jsonResp struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal([]byte(response), &jsonResp); err == nil {
		response = jsonResp.Result
	}

	// Look for "WINNER: <name>" pattern
	lines := strings.Split(response, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(line), "WINNER:") {
			winner := strings.TrimSpace(strings.TrimPrefix(line, "WINNER:"))
			winner = strings.TrimSpace(strings.TrimPrefix(winner, "winner:"))
			// Clean up any markdown formatting
			winner = strings.Trim(winner, "`*_")
			// Verify it's a valid worktree
			for _, wt := range worktrees {
				if wt.Name == winner {
					return winner
				}
			}
		}
	}

	// Fallback: look for any worktree name mentioned as winner
	responseLower := strings.ToLower(response)
	for _, wt := range worktrees {
		// Check if this worktree is mentioned near "winner" or "best"
		if strings.Contains(responseLower, strings.ToLower(wt.Name)) {
			idx := strings.Index(responseLower, strings.ToLower(wt.Name))
			// Check surrounding context for winner-like words
			start := idx - 50
			if start < 0 {
				start = 0
			}
			end := idx + len(wt.Name) + 50
			if end > len(responseLower) {
				end = len(responseLower)
			}
			context := responseLower[start:end]
			if strings.Contains(context, "winner") || strings.Contains(context, "best") || strings.Contains(context, "recommend") {
				return wt.Name
			}
		}
	}

	return ""
}

func doAccept(worktreeName, gitRoot, autom8Path string, tasks []core.Task) error {
	worktreePath := filepath.Join(autom8Path, "worktrees", worktreeName)

	// Check if worktree exists
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		return fmt.Errorf("worktree '%s' not found", worktreeName)
	}

	// Get the branch name from the worktree
	branchCmd := exec.Command("git", "-C", worktreePath, "branch", "--show-current")
	branchOutput, err := branchCmd.Output()
	if err != nil {
		return fmt.Errorf("error getting branch name: %w", err)
	}
	branchName := strings.TrimSpace(string(branchOutput))

	if branchName == "" {
		return fmt.Errorf("could not determine branch name for worktree")
	}

	// Check for uncommitted changes in the worktree
	statusCmd := exec.Command("git", "-C", worktreePath, "status", "--porcelain")
	statusOutput, err := statusCmd.Output()
	if err != nil {
		return fmt.Errorf("error checking worktree status: %w", err)
	}

	if len(strings.TrimSpace(string(statusOutput))) > 0 {
		// Stage all changes
		addCmd := exec.Command("git", "-C", worktreePath, "add", "-A")
		if _, err := addCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("error staging changes: %w", err)
		}

		// Commit with auto-commit message
		commitCmd := exec.Command("git", "-C", worktreePath, "commit", "-m", "autom8: auto-commit uncommitted changes")
		if _, err := commitCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("error committing changes: %w", err)
		}
	}

	// Merge the branch into the current branch
	mergeCmd := exec.Command("git", "-C", gitRoot, "merge", branchName, "-m", fmt.Sprintf("Merge %s (autom8 converge)", branchName))
	if output, err := mergeCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("error merging branch: %w\n%s", err, string(output))
	}

	// Remove the worktree
	removeCmd := exec.Command("git", "-C", gitRoot, "worktree", "remove", worktreePath)
	if _, err := removeCmd.CombinedOutput(); err != nil {
		// Non-fatal, continue
	}

	// Delete the branch
	deleteBranchCmd := exec.Command("git", "-C", gitRoot, "branch", "-d", branchName)
	deleteBranchCmd.Run()

	// Mark the task as completed
	// Build task ID set for worktree name matching
	taskIDs := make(map[string]struct{})
	for _, t := range tasks {
		taskIDs[t.ID] = struct{}{}
	}

	// Extract task ID from worktree name using proper matching
	taskID, ok := core.TaskIDFromWorktree(worktreeName, taskIDs)
	if !ok {
		return nil
	}

	for i, t := range tasks {
		if t.ID == taskID {
			tasks[i].Status = core.TaskStatusCompleted
			break
		}
	}

	return nil
}
