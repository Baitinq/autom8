package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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

	taskIndex := core.FindTaskIndex(tasks, workerTaskID)
	if taskIndex == -1 {
		return fmt.Errorf("task not found: %s", workerTaskID)
	}
	task := tasks[taskIndex]

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

		// Write status: implementing with iteration count
		implStatus := &core.WorktreeStatus{
			Status:    core.WorktreePhaseImplementing,
			Iteration: iteration,
		}
		core.WriteWorktreeStatus(worktreeName, implStatus)

		// Create log file for this iteration
		logFile := filepath.Join(logsDir, fmt.Sprintf("iteration-%d.log", iteration))

		// Run claude synchronously and stream output to file in real-time
		claudeCmd := exec.Command("claude", "-p", prompt, "--dangerously-skip-permissions")
		claudeCmd.Dir = workerWorktreePath

		output, err := runCommandWithStreaming(claudeCmd, logFile)
		if err != nil {
			return fmt.Errorf("implementation iteration %d failed: %w", iteration, err)
		}

		// Check if output contains TASK COMPLETE signal
		if strings.Contains(string(output), "<output>TASK COMPLETE</output>") {
			// Implementation complete - now start the review loop
			reviewErr := runWorkerReviewLoop(task, workerWorktreePath, logsDir, workerBaseBranch)
			if reviewErr != nil {
				return fmt.Errorf("review failed: %w", reviewErr)
			}

			// Mark this worktree as ready by writing status file
			status := &core.WorktreeStatus{
				Status:      core.WorktreePhaseReady,
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
// The reviewer agent will directly apply any fixes it finds.
func runWorkerReviewLoop(task core.Task, worktreePath, logsDir, baseBranch string) error {
	// Extract worktree name from path for status updates
	worktreeName := filepath.Base(worktreePath)

	reviewIteration := 0

	for {
		reviewIteration++

		// Write status: reviewing with iteration count
		reviewStatus := &core.WorktreeStatus{
			Status:       core.WorktreePhaseReviewing,
			FixIteration: reviewIteration - 1, // Show how many review passes have occurred
		}
		core.WriteWorktreeStatus(worktreeName, reviewStatus)

		// Create log file for this review iteration
		reviewLogFile := filepath.Join(logsDir, fmt.Sprintf("review-iteration-%d.log", reviewIteration))

		// Build the review prompt with reviewer.md template, task info, and diff
		reviewPrompt, err := buildReviewPrompt(task, worktreePath, baseBranch)
		if err != nil {
			os.WriteFile(reviewLogFile, []byte(fmt.Sprintf("ERROR building prompt: %v", err)), 0644)
			return fmt.Errorf("failed to build review prompt: %w", err)
		}

		// Run codex exec with the review prompt via stdin to avoid argv length limits
		// Stream output to file in real-time
		codexCmd := exec.Command("codex", "exec", "--dangerously-bypass-approvals-and-sandbox", "-")
		codexCmd.Dir = worktreePath
		codexCmd.Stdin = strings.NewReader(reviewPrompt)

		output, err := runCommandWithStreaming(codexCmd, reviewLogFile)
		if err != nil {
			return fmt.Errorf("review iteration %d failed: %w", reviewIteration, err)
		}

		// Check if review is complete (reviewer either found no issues or applied all fixes)
		if strings.Contains(string(output), "<output>REVIEW COMPLETE</output>") {
			return nil // Success - review complete
		}

		// Check if review is blocked (reviewer found issues that require reimplementation)
		if strings.Contains(string(output), "<output>REVIEW BLOCKED</output>") {
			return fmt.Errorf("review blocked in iteration %d", reviewIteration)
		}

		// Reviewer made changes or found issues it couldn't fix - loop back for another review pass
	}
}

// runCommandWithStreaming runs a command and streams its output to a log file in real-time
// while also capturing the full output in memory for processing.
// Returns the captured output and any error from the command execution.
func runCommandWithStreaming(cmd *exec.Cmd, logFile string) ([]byte, error) {
	// Open log file for streaming output
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}
	defer f.Close()

	// Get stdout and stderr pipes
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start command: %w", err)
	}

	// Buffer to capture full output for processing
	var outputBuf bytes.Buffer

	// Create a MultiWriter to write to both the file and buffer
	multiWriter := io.MultiWriter(f, &outputBuf)
	lockedWriter := &lockedWriter{w: multiWriter}

	// Use goroutines to copy stdout and stderr concurrently
	errChan := make(chan error, 2)

	go func() {
		_, err := io.Copy(lockedWriter, stdout)
		errChan <- err
	}()

	go func() {
		_, err := io.Copy(lockedWriter, stderr)
		errChan <- err
	}()

	// Wait for both copy operations to complete
	copyErr1 := <-errChan
	copyErr2 := <-errChan

	// Wait for command to finish
	cmdErr := cmd.Wait()

	// Check for errors (prefer command error over copy errors)
	if cmdErr != nil {
		// Write error to log file
		fmt.Fprintf(f, "\n\nERROR: %v\n", cmdErr)
		return outputBuf.Bytes(), fmt.Errorf("command failed: %w", cmdErr)
	}

	if copyErr1 != nil {
		return outputBuf.Bytes(), fmt.Errorf("error copying stdout: %w", copyErr1)
	}
	if copyErr2 != nil {
		return outputBuf.Bytes(), fmt.Errorf("error copying stderr: %w", copyErr2)
	}

	return outputBuf.Bytes(), nil
}

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (lw *lockedWriter) Write(p []byte) (int, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	return lw.w.Write(p)
}

// buildReviewPrompt constructs the prompt for the reviewer agent.
func buildReviewPrompt(task core.Task, worktreePath, baseBranch string) (string, error) {
	var sb strings.Builder

	// Load reviewer template
	reviewerTemplate, err := loadAgentTemplate("reviewer")
	if err != nil {
		return "", fmt.Errorf("failed to load reviewer template: %w", err)
	}

	sb.WriteString(reviewerTemplate)
	sb.WriteString("\n")
	sb.WriteString(task.Prompt)
	sb.WriteString("\n\n")

	if len(task.VerificationCriteria) > 0 {
		sb.WriteString("## Verification Criteria\n\n")
		for _, c := range task.VerificationCriteria {
			sb.WriteString(fmt.Sprintf("- %s\n", c))
		}
		sb.WriteString("\n")
	}

	// Get the diff against base branch
	diffCmd := exec.Command("git", "diff", baseBranch+"...HEAD")
	diffCmd.Dir = worktreePath
	diffOutput, err := diffCmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get diff: %w", err)
	}

	sb.WriteString("## Implementation Diff\n\n")
	sb.WriteString("```diff\n")
	sb.WriteString(string(diffOutput))
	sb.WriteString("```\n")

	return sb.String(), nil
}
