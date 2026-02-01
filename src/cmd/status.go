package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/baitinq/autom8/src/core"
	"github.com/spf13/cobra"
)

var StatusCmd = &cobra.Command{
	Use:     "status",
	Aliases: []string{"ls", "list"},
	Short:   "Show tasks and their worktrees in a tree view",
	Long: `Display all tasks with their dependencies and associated worktrees.

Shows a tree structure with:
  - Task status, prompt, and verification criteria
  - Dependent tasks nested under their parents
  - Worktrees for each task with their git status
  - Hints for accepting completed implementations`,
	RunE: runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	if _, err := core.GetGitRoot(); err != nil {
		return err
	}

	tasks, err := core.LoadTasks()
	if err != nil {
		return fmt.Errorf("error loading tasks: %w", err)
	}

	// Get worktrees and PIDs
	autom8Path, _ := core.GetAutom8Dir()
	worktreesDir := filepath.Join(autom8Path, "worktrees")
	worktreesByTask := make(map[string][]core.WorktreeInfo)
	pids, _ := core.LoadPids()

	if entries, err := os.ReadDir(worktreesDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			worktreeName := entry.Name()
			// Extract task ID: {task-name}-{instance} -> {task-name}
			taskID := worktreeName
			if lastDash := strings.LastIndex(worktreeName, "-"); lastDash > 0 {
				taskID = worktreeName[:lastDash]
			}
			info := core.GetWorktreeInfo(worktreesDir, worktreeName, pids)
			worktreesByTask[taskID] = append(worktreesByTask[taskID], info)
		}
	}

	if len(tasks) == 0 {
		fmt.Println(SubtitleStyle.Render("No tasks found. Use 'autom8 new' to create one."))
		return nil
	}

	// Build dependency tree
	taskMap := make(map[string]core.Task)
	childrenMap := make(map[string][]string) // parent ID -> child IDs
	var rootTasks []string

	for _, t := range tasks {
		taskMap[t.ID] = t
		if t.DependsOn == "" {
			rootTasks = append(rootTasks, t.ID)
		} else {
			childrenMap[t.DependsOn] = append(childrenMap[t.DependsOn], t.ID)
		}
	}

	fmt.Println(TitleStyle.Render("Status"))
	fmt.Println()

	// Print tree recursively
	var printTask func(taskID string, prefix string, isLast bool)
	printTask = func(taskID string, prefix string, isLast bool) {
		task := taskMap[taskID]

		// Tree branch characters
		branch := "├── "
		if isLast {
			branch = "└── "
		}
		childPrefix := prefix + "│   "
		if isLast {
			childPrefix = prefix + "    "
		}

		// Status badge
		var statusBadge string
		switch task.Status {
		case "pending":
			statusBadge = StatusPendingStyle.Render("[pending]")
		case "in-progress":
			statusBadge = StatusInProgressStyle.Render("[in-progress]")
		case "ready":
			statusBadge = StatusReadyStyle.Render("[ready]")
		case "completed":
			statusBadge = StatusCompletedStyle.Render("[completed]")
		default:
			statusBadge = SubtitleStyle.Render(fmt.Sprintf("[%s]", task.Status))
		}

		// Print task header: name at top, prompt below
		fmt.Printf("%s%s%s %s\n", prefix, branch, statusBadge, IDStyle.Render(task.ID))
		fmt.Printf("%s%s %s\n", childPrefix, SubtitleStyle.Render("Prompt:"), core.Truncate(task.Prompt, 60))

		// Print verification criteria
		if len(task.VerificationCriteria) > 0 {
			fmt.Printf("%s%s\n", childPrefix, SubtitleStyle.Render("Criteria:"))
			for _, c := range task.VerificationCriteria {
				fmt.Printf("%s  • %s\n", childPrefix, c)
			}
		}

		// Print worktrees for this task
		worktrees := worktreesByTask[task.ID]
		children := childrenMap[task.ID]
		hasMore := len(children) > 0

		if len(worktrees) > 0 {
			fmt.Printf("%s%s\n", childPrefix, SubtitleStyle.Render("Worktrees:"))
			for i, wt := range worktrees {
				wtIsLast := i == len(worktrees)-1 && !hasMore
				wtBranch := "├── "
				if wtIsLast {
					wtBranch = "└── "
				}

				// Worktree status
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

				fmt.Printf("%s%s%s %s\n", childPrefix, wtBranch, wtStatus, wt.Name)

				// Show accept hint
				if !wt.IsRunning && (wt.CommitsAhead != "0" || wt.HasChanges) {
					wtChildPrefix := childPrefix + "│   "
					if wtIsLast {
						wtChildPrefix = childPrefix + "    "
					}
					fmt.Printf("%s%s autom8 accept %s\n", wtChildPrefix, HighlightStyle.Render("→"), wt.Name)
				}
			}
		} else if task.Status == "pending" {
			fmt.Printf("%s%s\n", childPrefix, SubtitleStyle.Render("(no worktrees - run 'autom8 implement')"))
		}

		// Print children (dependent tasks)
		for i, childID := range children {
			printTask(childID, childPrefix, i == len(children)-1)
		}
	}

	// Print all root tasks
	for i, taskID := range rootTasks {
		printTask(taskID, "", i == len(rootTasks)-1)
		if i < len(rootTasks)-1 {
			fmt.Println()
		}
	}

	fmt.Println()
	return nil
}
