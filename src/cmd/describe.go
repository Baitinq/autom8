package cmd

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/baitinq/autom8/src/core"
	"github.com/spf13/cobra"
)

var DescribeCmd = &cobra.Command{
	Use:     "describe <task-name>",
	Aliases: []string{"info"},
	Short:   "Show detailed information about a task",
	Long: `Display detailed information about a specific task.

Shows comprehensive task details including:
  - Task name and creation time
  - Full prompt text
  - All verification criteria
  - Dependency information
  - Current status
  - Associated worktrees and their state`,
	Example: `  autom8 describe my-task`,
	Args:    cobra.ExactArgs(1),
	RunE:    runDescribe,
}

func runDescribe(cmd *cobra.Command, args []string) error {
	taskID := args[0]

	if _, err := core.GetGitRoot(); err != nil {
		return err
	}

	tasks, err := core.LoadTasks()
	if err != nil {
		return fmt.Errorf("error loading tasks: %w", err)
	}

	taskIDs := make(map[string]struct{}, len(tasks))
	for _, t := range tasks {
		taskIDs[t.ID] = struct{}{}
	}

	taskIndex := core.FindTaskIndex(tasks, taskID)
	if taskIndex == -1 {
		return fmt.Errorf("task '%s' not found\nRun 'autom8 status' to see task names", taskID)
	}
	task := tasks[taskIndex]

	// Build task map for dependency lookup
	taskMap := make(map[string]core.Task)
	for _, t := range tasks {
		taskMap[t.ID] = t
	}

	// Find dependent tasks
	var dependents []string
	for _, t := range tasks {
		if t.DependsOn == taskID {
			dependents = append(dependents, t.ID)
		}
	}

	// Get worktrees for this task
	worktreesDir, err := core.GetWorktreesDir()
	if err != nil {
		return err
	}
	pids, _ := core.LoadPids()
	worktrees := core.ListWorktreesByTask(worktreesDir, taskIDs, pids)[taskID]

	// Display task information
	fmt.Println(TitleStyle.Render("Task Details"))
	fmt.Println()

	// Status badge
	var statusBadge string
	switch task.Status {
	case core.TaskStatusPending:
		statusBadge = StatusPendingStyle.Render("[pending]")
	case core.TaskStatusInProgress:
		statusBadge = StatusInProgressStyle.Render("[in-progress]")
	case core.TaskStatusCompleted:
		statusBadge = StatusCompletedStyle.Render("[completed]")
	default:
		statusBadge = SubtitleStyle.Render(fmt.Sprintf("[%s]", string(task.Status)))
	}

	fmt.Printf("  %s %s\n", SubtitleStyle.Render("Name:"), NameStyle.Render(task.ID))
	// Show type for investigation tasks
	if task.GetType() == core.TaskTypeInvestigation {
		fmt.Printf("  %s %s\n", SubtitleStyle.Render("Type:"), HighlightStyle.Render("investigation"))
	}
	fmt.Printf("  %s %s\n", SubtitleStyle.Render("Status:"), statusBadge)
	fmt.Printf("  %s %s\n", SubtitleStyle.Render("Created:"), task.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Println()

	// Prompt (full, not truncated)
	fmt.Println(SubtitleStyle.Render("  Prompt:"))
	for _, line := range strings.Split(task.Prompt, "\n") {
		fmt.Printf("    %s\n", line)
	}
	fmt.Println()

	// Verification criteria
	if len(task.VerificationCriteria) > 0 {
		fmt.Println(SubtitleStyle.Render("  Verification Criteria:"))
		for i, c := range task.VerificationCriteria {
			fmt.Printf("    %d. %s\n", i+1, c)
		}
		fmt.Println()
	}

	// Dependencies
	if task.DependsOn != "" {
		parentTask := taskMap[task.DependsOn]
		fmt.Println(SubtitleStyle.Render("  Depends On:"))
		fmt.Printf("    %s - %s\n", NameStyle.Render(task.DependsOn), core.Truncate(parentTask.Prompt, 50))
		fmt.Println()
	}

	// Dependent tasks
	if len(dependents) > 0 {
		fmt.Println(SubtitleStyle.Render("  Dependents:"))
		for _, depID := range dependents {
			depTask := taskMap[depID]
			fmt.Printf("    %s - %s\n", NameStyle.Render(depID), core.Truncate(depTask.Prompt, 50))
		}
		fmt.Println()
	}

	// Worktrees
	var readyWorktrees []core.WorktreeInfo
	if len(worktrees) > 0 {
		fmt.Println(SubtitleStyle.Render("  Worktrees:"))
		for _, wt := range worktrees {
			var wtStatus string
			if wt.IsRunning && wt.Phase == core.WorktreePhaseImplementing {
				wtStatus = StatusInProgressStyle.Render(fmt.Sprintf("[implementing (%d)]", wt.Iteration))
			} else if wt.IsRunning && wt.Phase == core.WorktreePhaseReviewing {
				if wt.FixIteration > 0 {
					wtStatus = StatusInProgressStyle.Render(fmt.Sprintf("[reviewing (fix %d)]", wt.FixIteration))
				} else {
					wtStatus = StatusInProgressStyle.Render("[reviewing]")
				}
			} else if wt.IsRunning {
				// Fallback for running without phase info
				wtStatus = StatusInProgressStyle.Render("[running]")
			} else if wt.Phase == core.WorktreePhaseReady {
				wtStatus = StatusReadyStyle.Render("[ready]")
				readyWorktrees = append(readyWorktrees, wt)
			} else if wt.Phase == core.WorktreePhaseImplementing || wt.Phase == core.WorktreePhaseReviewing {
				// Non-running worktree with implementing/reviewing phase = error (worker died while working)
				wtStatus = StatusErrorStyle.Render("[error]")
			} else {
				wtStatus = SubtitleStyle.Render("[idle]")
			}
			fmt.Printf("    %s %s\n", wtStatus, NameStyle.Render(wt.Name))
			fmt.Printf("      %s %s\n", SubtitleStyle.Render("Branch:"), HighlightStyle.Render(wt.Branch))
			fmt.Printf("      %s %s\n", SubtitleStyle.Render("Path:"), wt.Path)
			// Show accept hint only for ready worktrees
			if !wt.IsRunning && wt.Phase == core.WorktreePhaseReady {
				fmt.Printf("      %s autom8 accept %s\n", HighlightStyle.Render("→"), wt.Name)
			}
		}
	} else if task.Status == core.TaskStatusPending {
		fmt.Println(SubtitleStyle.Render("  Worktrees:"))
		fmt.Println("    (none - run 'autom8 implement' to start)")
	}

	// Show comparison if multiple ready worktrees exist
	if len(readyWorktrees) > 1 {
		fmt.Println()
		fmt.Println(SubtitleStyle.Render("  Implementation Comparison:"))
		comparison := compareWorktrees(task, readyWorktrees)
		if comparison != "" {
			for _, line := range strings.Split(comparison, "\n") {
				fmt.Printf("    %s\n", line)
			}
		} else {
			fmt.Println("    (could not generate comparison)")
		}
	}

	fmt.Println()
	return nil
}

// compareWorktrees uses Claude AI to analyze and compare multiple worktree implementations.
func compareWorktrees(task core.Task, worktrees []core.WorktreeInfo) string {
	prompt := buildComparePrompt(task, worktrees)

	claudeCmd := exec.Command("claude", "-p", "-", "--output-format", "json")
	claudeCmd.Stdin = strings.NewReader(prompt)

	output, err := claudeCmd.Output()
	if err != nil {
		return ""
	}

	return parseCompareResponse(string(output))
}

func buildComparePrompt(task core.Task, worktrees []core.WorktreeInfo) string {
	var sb strings.Builder

	sb.WriteString("Compare these implementations of the same task and summarize the key differences.\n\n")

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

	for _, wt := range worktrees {
		sb.WriteString(fmt.Sprintf("### %s\n\n", wt.Name))

		diffCmd := exec.Command("git", "-C", wt.Path, "diff", "@{u}...HEAD")
		diffOutput, err := diffCmd.Output()
		if err != nil || len(diffOutput) == 0 {
			sb.WriteString("(no changes)\n\n")
			continue
		}

		diff := string(diffOutput)
		if len(diff) > 30000 {
			diff = diff[:30000] + "\n... (truncated)"
		}
		sb.WriteString("```diff\n")
		sb.WriteString(diff)
		sb.WriteString("\n```\n\n")
	}

	sb.WriteString("## Your Task\n\n")
	sb.WriteString("Provide a BRIEF comparison (2-3 sentences per implementation) highlighting:\n")
	sb.WriteString("- Key differences in approach\n")
	sb.WriteString("- Trade-offs between implementations\n\n")
	sb.WriteString("Format your response with a short summary for each worktree:\n")
	sb.WriteString("<output>COMPARISON:\n")
	for _, wt := range worktrees {
		sb.WriteString(fmt.Sprintf("- %s: [2-3 sentence summary]\n", wt.Name))
	}
	sb.WriteString("</output>\n")

	return sb.String()
}

func parseCompareResponse(response string) string {
	// Parse JSON response
	var jsonResp struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal([]byte(response), &jsonResp); err == nil {
		response = jsonResp.Result
	}

	// Look for "<output>COMPARISON:" pattern
	if start := strings.Index(response, "<output>COMPARISON:"); start != -1 {
		start += len("<output>COMPARISON:")
		if end := strings.Index(response[start:], "</output>"); end != -1 {
			return strings.TrimSpace(response[start : start+end])
		}
	}

	return ""
}
