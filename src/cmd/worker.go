package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/baitinq/autom8/src/core"
	"github.com/spf13/cobra"
)

const (
	WorkerLogFile = "worker.log"
)

var (
	workerWorktreePath string
	workerBaseBranch   string
	workerTaskID       string
	workerMaxIter      int
)

// WorkerCmd is a hidden command that runs implementation and review loops for a single worktree.
// It is spawned by the implement command and runs in the background.
var WorkerCmd = &cobra.Command{
	Use:    "_worker",
	Short:  "Internal worker for running implementation loops",
	Hidden: true,
	RunE:   runWorker,
}

func init() {
	WorkerCmd.Flags().StringVar(&workerWorktreePath, "worktree-path", "", "Path to the worktree directory")
	WorkerCmd.Flags().StringVar(&workerBaseBranch, "base-branch", "main", "Base branch for review")
	WorkerCmd.Flags().StringVar(&workerTaskID, "task-id", "", "Task ID being implemented")
	WorkerCmd.Flags().IntVar(&workerMaxIter, "max-iterations", 0, "Maximum iterations (0 = unlimited)")
	WorkerCmd.MarkFlagRequired("worktree-path")
	WorkerCmd.MarkFlagRequired("task-id")
}

func runWorker(cmd *cobra.Command, args []string) error {
	// Validate worktree path exists
	if _, err := os.Stat(workerWorktreePath); os.IsNotExist(err) {
		return fmt.Errorf("worktree path does not exist: %s", workerWorktreePath)
	}

	// Extract worktree name from path
	worktreeName := filepath.Base(workerWorktreePath)

	// Set up logging directory first (needed for PID file)
	autom8Path, err := core.GetAutom8Dir()
	if err != nil {
		return err
	}
	logsDir := filepath.Join(autom8Path, "logs", worktreeName)
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return fmt.Errorf("failed to create logs dir: %w", err)
	}

	// Write PID file to logs directory (not worktree, to avoid git status noise)
	pidFile := filepath.Join(logsDir, core.WorkerPidFile)
	pid := os.Getpid()
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(pid)), 0644); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	// Ensure PID file is removed on exit (success or failure)
	defer os.Remove(pidFile)

	// Load the task
	tasks, err := core.LoadTasks()
	if err != nil {
		return fmt.Errorf("error loading tasks: %w", err)
	}

	var task core.Task
	found := false
	for _, t := range tasks {
		if t.ID == workerTaskID {
			task = t
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("task not found: %s", workerTaskID)
	}

	// Load the implementer agent template
	agentTemplate, err := loadAgentTemplate("implementer")
	if err != nil {
		agentTemplate = ""
	}

	// Build the prompt with agent template, task, and verification criteria
	var promptBuilder strings.Builder
	if agentTemplate != "" {
		promptBuilder.WriteString(agentTemplate)
	}
	promptBuilder.WriteString(task.Prompt)
	if len(task.VerificationCriteria) > 0 {
		promptBuilder.WriteString("\n\n## Verification Criteria\n\n")
		for _, c := range task.VerificationCriteria {
			promptBuilder.WriteString(fmt.Sprintf("- %s\n", c))
		}
	}
	prompt := promptBuilder.String()

	// Run implementation loop
	iteration := 0
	for {
		iteration++

		// Check max iterations limit
		if workerMaxIter > 0 && iteration > workerMaxIter {
			return fmt.Errorf("max iterations %d reached", workerMaxIter)
		}

		// Create log file for this iteration
		logFile := filepath.Join(logsDir, fmt.Sprintf("iteration-%d.log", iteration))

		// Run claude synchronously and capture output
		claudeCmd := exec.Command("claude", "-p", prompt, "--dangerously-skip-permissions")
		claudeCmd.Dir = workerWorktreePath

		output, err := claudeCmd.Output()
		if err != nil {
			// Log the error
			os.WriteFile(logFile, []byte(fmt.Sprintf("ERROR: %v\n%s", err, string(output))), 0644)
			return fmt.Errorf("implementation iteration %d failed: %w", iteration, err)
		}

		// Write output to log file
		os.WriteFile(logFile, output, 0644)

		// Check if output contains TASK COMPLETE
		if strings.Contains(string(output), "TASK COMPLETE") {
			// Implementation complete - now start the review loop
			reviewErr := runWorkerReviewLoop(task, workerWorktreePath, logsDir, workerBaseBranch)
			if reviewErr != nil {
				return fmt.Errorf("review failed: %w", reviewErr)
			}

			// Mark this worktree as ready by writing status file
			status := &core.WorktreeStatus{
				Status:      "ready",
				CompletedAt: time.Now(),
			}
			if err := core.WriteWorktreeStatus(worktreeName, status); err != nil {
				return fmt.Errorf("failed to write worktree status: %w", err)
			}
			return nil // Success
		}

		// Continue to next iteration
	}
}

// runWorkerReviewLoop runs the review loop after implementation completes.
func runWorkerReviewLoop(task core.Task, worktreePath, logsDir, baseBranch string) error {
	reviewIteration := 0
	fixIteration := 0

	for {
		reviewIteration++

		// Create log file for this review iteration
		reviewLogFile := filepath.Join(logsDir, fmt.Sprintf("review-iteration-%d.log", reviewIteration))

		// Run codex review with base branch
		// Note: codex review --base doesn't accept a prompt argument
		codexCmd := exec.Command("codex", "review", "--base", baseBranch)
		codexCmd.Dir = worktreePath

		output, err := codexCmd.CombinedOutput()
		if err != nil {
			// Log the error with full output
			os.WriteFile(reviewLogFile, []byte(fmt.Sprintf("ERROR: %v\n\nOutput:\n%s", err, string(output))), 0644)
			return fmt.Errorf("review iteration %d failed: %w", reviewIteration, err)
		}

		// Write output to log file
		os.WriteFile(reviewLogFile, output, 0644)

		// Check if review is approved
		if strings.Contains(string(output), "REVIEW APPROVED") {
			return nil // Success - review approved
		}

		// Review found issues - run fix iteration
		fixIteration++

		// Build fix prompt with reviewer feedback
		fixPrompt := buildFixPrompt(task, string(output))

		// Create log file for this fix iteration
		fixLogFile := filepath.Join(logsDir, fmt.Sprintf("fix-iteration-%d.log", fixIteration))

		// Run codex exec to fix issues
		fixCmd := exec.Command("codex", "exec", "--dangerously-bypass-approvals-and-sandbox", fixPrompt)
		fixCmd.Dir = worktreePath

		fixOutput, err := fixCmd.CombinedOutput()
		if err != nil {
			// Log the error with full output
			os.WriteFile(fixLogFile, []byte(fmt.Sprintf("ERROR: %v\n\nOutput:\n%s", err, string(fixOutput))), 0644)
			return fmt.Errorf("fix iteration %d failed: %w", fixIteration, err)
		}

		// Write output to log file
		os.WriteFile(fixLogFile, fixOutput, 0644)

		// Continue to next review iteration
	}
}

// buildFixPrompt constructs the prompt for fixing issues based on reviewer feedback.
func buildFixPrompt(task core.Task, reviewerFeedback string) string {
	var sb strings.Builder

	// Load implementer template for context
	implementerTemplate, _ := loadAgentTemplate("implementer")
	if implementerTemplate != "" {
		sb.WriteString(implementerTemplate)
	}

	sb.WriteString("## Original Task\n\n")
	sb.WriteString(task.Prompt)
	sb.WriteString("\n\n")

	if len(task.VerificationCriteria) > 0 {
		sb.WriteString("## Verification Criteria\n\n")
		for _, c := range task.VerificationCriteria {
			sb.WriteString(fmt.Sprintf("- %s\n", c))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Reviewer Feedback\n\n")
	sb.WriteString("The code review found the following issues that need to be fixed:\n\n")
	sb.WriteString(reviewerFeedback)
	sb.WriteString("\n\n")

	sb.WriteString("## Your Task\n\n")
	sb.WriteString("Fix the issues identified by the reviewer. Make the necessary changes to satisfy all verification criteria.\n")
	sb.WriteString("After making fixes, commit your changes with a descriptive message.\n")

	return sb.String()
}
