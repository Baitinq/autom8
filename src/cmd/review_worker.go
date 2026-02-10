package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/baitinq/autom8/src/core"
	"github.com/spf13/cobra"
)

var (
	reviewWorkerTaskID string
)

// ReviewWorkerCmd is a hidden command that runs the review process for a review task.
// It is spawned by the implement command and runs in the background.
var ReviewWorkerCmd = &cobra.Command{
	Use:    "_review-worker",
	Short:  "Internal worker for running code reviews",
	Hidden: true,
	RunE:   runReviewWorker,
}

func init() {
	ReviewWorkerCmd.Flags().StringVar(&reviewWorkerTaskID, "task-id", "", "Task ID to review")
	ReviewWorkerCmd.MarkFlagRequired("task-id")
}

func runReviewWorker(cmd *cobra.Command, args []string) error {
	// Get git root
	gitRoot, err := core.GetGitRoot()
	if err != nil {
		return err
	}

	// Set up logging directory
	baseLogsDir, err := core.GetLogsDir()
	if err != nil {
		return err
	}
	logsDir := filepath.Join(baseLogsDir, reviewWorkerTaskID)
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return fmt.Errorf("failed to create logs dir: %w", err)
	}

	// Write PID file
	pidFile := filepath.Join(logsDir, core.WorkerPidFile)
	pid := os.Getpid()
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(pid)), 0644); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}
	defer os.Remove(pidFile)

	// Load the task
	tasks, err := core.LoadTasks()
	if err != nil {
		return fmt.Errorf("error loading tasks: %w", err)
	}

	taskIndex := core.FindTaskIndex(tasks, reviewWorkerTaskID)
	if taskIndex == -1 {
		return fmt.Errorf("task not found: %s", reviewWorkerTaskID)
	}
	task := tasks[taskIndex]

	if task.GetType() != core.TaskTypeReview {
		return fmt.Errorf("task '%s' is not a review task", reviewWorkerTaskID)
	}

	// Write status: reviewing
	reviewStatus := &core.WorktreeStatus{
		Status: core.WorktreePhaseReviewing,
	}
	core.WriteWorktreeStatus(reviewWorkerTaskID, reviewStatus)

	// Create a logger function for ExecuteReviewTask
	logFunc := func(format string, args ...interface{}) {
		fmt.Printf(format+"\n", args...)
	}

	// Run the review
	_, err = ExecuteReviewTask(task, gitRoot, logFunc)
	if err != nil {
		return fmt.Errorf("review failed: %w", err)
	}

	// Mark as completed
	status := &core.WorktreeStatus{
		Status:      core.WorktreePhaseReady,
		CompletedAt: time.Now(),
	}
	if err := core.WriteWorktreeStatus(reviewWorkerTaskID, status); err != nil {
		return fmt.Errorf("failed to write status: %w", err)
	}

	// Mark task as completed
	tasks[taskIndex].Status = core.TaskStatusCompleted
	if err := core.SaveTasks(tasks); err != nil {
		return fmt.Errorf("failed to update task status: %w", err)
	}

	fmt.Println("Review task completed successfully!")
	return nil
}
