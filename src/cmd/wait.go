package cmd

import (
	"fmt"
	"time"

	"github.com/baitinq/autom8/src/core"
	"github.com/spf13/cobra"
)

var waitAll bool

var WaitCmd = &cobra.Command{
	Use:   "wait [task-name]",
	Short: "Wait for worktrees to complete",
	Long: `Wait until all worktrees for specified tasks reach ready or idle state.

Examples:
  autom8 wait my-task    # Wait for a specific task's worktrees
  autom8 wait --all      # Wait for all in-progress tasks`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWait,
}

func init() {
	WaitCmd.Flags().BoolVarP(&waitAll, "all", "a", false, "Wait for all in-progress tasks")
}

func runWait(cmd *cobra.Command, args []string) error {
	if _, err := core.GetGitRoot(); err != nil {
		return err
	}

	// Validate arguments
	if len(args) == 0 && !waitAll {
		return fmt.Errorf("must specify a task name or use --all")
	}
	if len(args) > 0 && waitAll {
		return fmt.Errorf("cannot specify both a task name and --all")
	}

	tasks, err := core.LoadTasks()
	if err != nil {
		return fmt.Errorf("error loading tasks: %w", err)
	}

	// Determine which task IDs to wait for
	var waitTaskIDs []string
	if waitAll {
		for _, t := range tasks {
			if t.Status == core.TaskStatusInProgress {
				waitTaskIDs = append(waitTaskIDs, t.ID)
			}
		}
		if len(waitTaskIDs) == 0 {
			// No in-progress tasks, nothing to wait for
			return nil
		}
	} else {
		taskName := args[0]
		found := false
		for _, t := range tasks {
			if t.ID == taskName {
				found = true
				waitTaskIDs = append(waitTaskIDs, t.ID)
				break
			}
		}
		if !found {
			return fmt.Errorf("task '%s' not found", taskName)
		}
	}

	// Build task ID set for worktree lookup
	allTaskIDs := make(map[string]struct{})
	for _, t := range tasks {
		allTaskIDs[t.ID] = struct{}{}
	}

	// Poll until all worktrees are done
	for {
		worktreesDir, err := core.GetWorktreesDir()
		if err != nil {
			return err
		}
		pids, _ := core.LoadPids()
		worktreesByTask := core.ListWorktreesByTask(worktreesDir, allTaskIDs, pids)

		allDone := true
		for _, taskID := range waitTaskIDs {
			worktrees := worktreesByTask[taskID]
			for _, wt := range worktrees {
				if !isWorktreeDone(wt) {
					allDone = false
					break
				}
			}
			if !allDone {
				break
			}
		}

		if allDone {
			return nil
		}

		time.Sleep(2 * time.Second)
	}
}

// isWorktreeDone returns true if the worktree has finished processing.
// A worktree is done when it's in ready or idle state.
func isWorktreeDone(wt core.WorktreeInfo) bool {
	return wt.Phase == core.WorktreePhaseReady || wt.Phase == core.WorktreePhaseIdle
}
