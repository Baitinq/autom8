package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/baitinq/autom8/src/core"
	"github.com/spf13/cobra"
)

var DescribeCmd = &cobra.Command{
	Use:   "describe <task-id>",
	Short: "Show detailed information about a task",
	Long: `Display detailed information about a specific task.

Shows comprehensive task details including:
  - Task ID and creation time
  - Full prompt text
  - All verification criteria
  - Dependency information
  - Current status
  - Associated worktrees and their state`,
	Example: `  autom8 describe task-123456789`,
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

	// Find the task
	var task *core.Task
	for i := range tasks {
		if tasks[i].ID == taskID {
			task = &tasks[i]
			break
		}
	}

	if task == nil {
		return fmt.Errorf("task '%s' not found\nRun 'autom8 status' to see task IDs", taskID)
	}

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
	autom8Path, _ := core.GetAutom8Dir()
	worktreesDir := filepath.Join(autom8Path, "worktrees")
	var worktrees []core.WorktreeInfo
	pids, _ := core.LoadPids()

	if entries, err := os.ReadDir(worktreesDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			worktreeName := entry.Name()
			// Extract task ID: task-{timestamp}-{instance} -> task-{timestamp}
			wtTaskID := worktreeName
			if lastDash := strings.LastIndex(worktreeName, "-"); lastDash > 0 {
				wtTaskID = worktreeName[:lastDash]
			}
			if wtTaskID == taskID {
				info := core.GetWorktreeInfo(worktreesDir, worktreeName, pids)
				worktrees = append(worktrees, info)
			}
		}
	}

	// Display task information
	fmt.Println(TitleStyle.Render("Task Details"))
	fmt.Println()

	// Status badge
	var statusBadge string
	switch task.Status {
	case "pending":
		statusBadge = StatusPendingStyle.Render("[pending]")
	case "in-progress":
		statusBadge = StatusInProgressStyle.Render("[in-progress]")
	case "completed":
		statusBadge = StatusCompletedStyle.Render("[completed]")
	default:
		statusBadge = SubtitleStyle.Render(fmt.Sprintf("[%s]", task.Status))
	}

	fmt.Printf("  %s %s\n", SubtitleStyle.Render("ID:"), IDStyle.Render(task.ID))
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
		fmt.Printf("    %s - %s\n", IDStyle.Render(task.DependsOn), core.Truncate(parentTask.Prompt, 50))
		fmt.Println()
	}

	// Dependent tasks
	if len(dependents) > 0 {
		fmt.Println(SubtitleStyle.Render("  Dependents:"))
		for _, depID := range dependents {
			depTask := taskMap[depID]
			fmt.Printf("    %s - %s\n", IDStyle.Render(depID), core.Truncate(depTask.Prompt, 50))
		}
		fmt.Println()
	}

	// Worktrees
	if len(worktrees) > 0 {
		fmt.Println(SubtitleStyle.Render("  Worktrees:"))
		for _, wt := range worktrees {
			var wtStatus string
			if wt.IsRunning {
				wtStatus = StatusInProgressStyle.Render("[running]")
			} else if wt.HasChanges {
				wtStatus = StatusPendingStyle.Render("[modified]")
			} else if wt.CommitsAhead != "0" {
				wtStatus = StatusCompletedStyle.Render("[" + wt.CommitsAhead + " commits]")
			} else {
				wtStatus = SubtitleStyle.Render("[idle]")
			}
			fmt.Printf("    %s %s\n", wtStatus, wt.Name)
			fmt.Printf("      %s %s\n", SubtitleStyle.Render("Branch:"), HighlightStyle.Render(wt.Branch))
			fmt.Printf("      %s %s\n", SubtitleStyle.Render("Path:"), wt.Path)
		}
	} else if task.Status == "pending" {
		fmt.Println(SubtitleStyle.Render("  Worktrees:"))
		fmt.Println("    (none - run 'autom8 implement' to start)")
	}

	fmt.Println()
	return nil
}
