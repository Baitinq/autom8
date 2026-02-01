package cmd

import (
	"fmt"
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

	if len(tasks) == 0 {
		fmt.Println(SubtitleStyle.Render("No tasks found. Use 'autom8 new' to create one."))
		return nil
	}

	// Build task ID set and dependency tree
	taskIDs := make(map[string]struct{})
	taskMap := make(map[string]core.Task)
	childrenMap := make(map[string][]string) // parent ID -> child IDs
	var rootTasks []string

	for _, t := range tasks {
		taskIDs[t.ID] = struct{}{}
		taskMap[t.ID] = t
	}

	for _, t := range tasks {
		if t.DependsOn == "" {
			rootTasks = append(rootTasks, t.ID)
		} else if _, parentExists := taskMap[t.DependsOn]; !parentExists {
			// Orphaned task (parent was deleted) - treat as root task
			rootTasks = append(rootTasks, t.ID)
		} else {
			childrenMap[t.DependsOn] = append(childrenMap[t.DependsOn], t.ID)
		}
	}

	// Get worktrees and PIDs
	worktreesDir, err := core.GetWorktreesDir()
	if err != nil {
		return err
	}
	pids, _ := core.LoadPids()
	worktreesByTask := core.ListWorktreesByTask(worktreesDir, taskIDs, pids)

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
		case core.TaskStatusPending:
			statusBadge = StatusPendingStyle.Render("[pending]")
		case core.TaskStatusInProgress:
			statusBadge = StatusInProgressStyle.Render("[in-progress]")
		case core.TaskStatusCompleted:
			statusBadge = StatusCompletedStyle.Render("[completed]")
		default:
			statusBadge = SubtitleStyle.Render(fmt.Sprintf("[%s]", string(task.Status)))
		}

		// Print task header: name at top, prompt below
		displayPrompt := core.Truncate(task.Prompt, 50)
		fmt.Printf("%s%s%s %s %s\n", prefix, branch, statusBadge, SubtitleStyle.Render("Name:"), NameStyle.Render(task.ID))
		fmt.Printf("%s%s %s\n", childPrefix, SubtitleStyle.Render("Prompt:"), displayPrompt)

		// Print verification criteria
		if len(task.VerificationCriteria) > 0 {
			fmt.Printf("%s%s\n", childPrefix, SubtitleStyle.Render("Criteria:"))
			for _, c := range task.VerificationCriteria {
				fmt.Printf("%s  • %s\n", childPrefix, c)
			}
		}

		// Show warning for orphaned tasks (parent was deleted)
		if task.DependsOn != "" {
			if _, parentExists := taskMap[task.DependsOn]; !parentExists {
				fmt.Printf("%s%s %s\n", childPrefix, ErrorStyle.Render("⚠"), SubtitleStyle.Render(fmt.Sprintf("(parent '%s' was deleted)", task.DependsOn)))
			}
		}

		// Print worktrees for this task
		worktrees := worktreesByTask[task.ID]
		children := childrenMap[task.ID]
		hasChildren := len(children) > 0

		if len(worktrees) > 0 {
			fmt.Printf("%s%s\n", childPrefix, SubtitleStyle.Render("Worktrees:"))
			for i, wt := range worktrees {
				// Keep branch continuity if child tasks follow the worktrees section.
				wtIsLast := i == len(worktrees)-1 && !hasChildren
				wtBranch := "├── "
				if wtIsLast {
					wtBranch = "└── "
				}

				// Worktree status
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
				} else if wt.HasChanges {
					wtStatus = StatusPendingStyle.Render("[modified]")
				} else if wt.Phase == core.WorktreePhaseReady {
					wtStatus = StatusReadyStyle.Render("[ready]")
				} else if wt.CommitsAhead != "0" {
					wtStatus = StatusCompletedStyle.Render("[" + wt.CommitsAhead + " commits]")
				} else {
					wtStatus = SubtitleStyle.Render("[idle]")
				}

				// Add star prefix for the winning worktree
				winnerPrefix := ""
				if task.Winner != "" && wt.Name == task.Winner {
					winnerPrefix = HighlightStyle.Render("★") + " "
				}
				fmt.Printf("%s%s%s %s%s\n", childPrefix, wtBranch, wtStatus, winnerPrefix, NameStyle.Render(wt.Name))

				// Show accept hint
				if !wt.IsRunning && (wt.Phase == core.WorktreePhaseReady || wt.CommitsAhead != "0" || wt.HasChanges) {
					wtChildPrefix := childPrefix + "│   "
					if wtIsLast {
						wtChildPrefix = childPrefix + "    "
					}
					fmt.Printf("%s%s autom8 accept %s\n", wtChildPrefix, HighlightStyle.Render("→"), wt.Name)
				}
			}
		} else if task.Status == core.TaskStatusPending && len(children) == 0 {
			fmt.Printf("%s%s\n", childPrefix, SubtitleStyle.Render("(no worktrees - run 'autom8 implement')"))
		}

		// Print children (dependent tasks) - these are separate from worktrees
		if len(children) > 0 && len(worktrees) > 0 {
			// Keep the tree guide visible between sections.
			fmt.Printf("%s\n", strings.TrimRight(childPrefix, " "))
		}
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
