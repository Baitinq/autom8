package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
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
	"github.com/creack/pty"
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
	WorkerCmd.Flags().StringVar(&workerBaseBranch, "base-branch", "", "Base branch for review (defaults to repo default)")
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
	baseLogsDir, err := core.GetLogsDir()
	if err != nil {
		return err
	}
	logsDir := filepath.Join(baseLogsDir, worktreeName)
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

		// Run claude with PTY for real-time streaming
		// PTY + --output-format stream-json + --include-partial-messages enables incremental output
		claudeCmd := exec.Command("claude",
			"-p", prompt,
			"--output-format", "stream-json",
			"--verbose",
			"--include-partial-messages",
			"--dangerously-skip-permissions")
		claudeCmd.Dir = workerWorktreePath

		output, err := runCommandWithPTYStreaming(claudeCmd, logFile)
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

// runCommandWithPTYStreaming runs a command with a PTY for real-time streaming output.
// This is used for claude implementation phase where PTY enables streaming JSON output.
// The function parses stream-json format and writes human-readable text to the log file.
func runCommandWithPTYStreaming(cmd *exec.Cmd, logFile string) ([]byte, error) {
	// Open log file for streaming output
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}
	defer f.Close()

	// Start the command with a PTY
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to start command with PTY: %w", err)
	}
	defer ptmx.Close()

	// Buffer to capture full raw output for TASK COMPLETE detection
	var rawOutputBuf bytes.Buffer

	// Read from PTY and parse JSON stream
	scanner := bufio.NewScanner(ptmx)
	// Increase buffer size for large JSON lines
	const maxScanTokenSize = 1024 * 1024 // 1MB
	buf := make([]byte, maxScanTokenSize)
	scanner.Buffer(buf, maxScanTokenSize)

	for scanner.Scan() {
		line := scanner.Text()
		rawOutputBuf.WriteString(line)
		rawOutputBuf.WriteString("\n")

		// Try to parse as JSON and extract text content for human-readable log
		text := extractTextFromStreamJSON(line)
		if text != "" {
			f.WriteString(text)
			f.Sync() // Flush immediately for real-time streaming
		}
	}

	// Handle scanner error
	if err := scanner.Err(); err != nil {
		// EOF is expected when process exits
		if err != io.EOF {
			fmt.Fprintf(f, "\n\nSCANNER ERROR: %v\n", err)
		}
	}

	// Wait for command to finish
	cmdErr := cmd.Wait()
	if cmdErr != nil {
		// Write error to log file
		fmt.Fprintf(f, "\n\nERROR: %v\n", cmdErr)
		return rawOutputBuf.Bytes(), fmt.Errorf("command failed: %w", cmdErr)
	}

	return rawOutputBuf.Bytes(), nil
}

// extractTextFromStreamJSON extracts human-readable text from claude's stream-json format.
// It handles various message types and extracts content appropriately.
func extractTextFromStreamJSON(line string) string {
	// Skip empty lines
	if strings.TrimSpace(line) == "" {
		return ""
	}

	// Parse as JSON
	var msg map[string]interface{}
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		// Not valid JSON - might be plain text output
		return line + "\n"
	}

	// Handle different message types in stream-json format
	msgType, _ := msg["type"].(string)

	switch msgType {
	case "assistant":
		// Main assistant message - extract content from message.content array
		if message, ok := msg["message"].(map[string]interface{}); ok {
			if content, ok := message["content"].([]interface{}); ok {
				var text strings.Builder
				for _, item := range content {
					if itemMap, ok := item.(map[string]interface{}); ok {
						if itemMap["type"] == "text" {
							if textContent, ok := itemMap["text"].(string); ok {
								text.WriteString(textContent)
							}
						}
					}
				}
				return text.String()
			}
		}
	case "content_block_delta":
		// Incremental text delta
		if delta, ok := msg["delta"].(map[string]interface{}); ok {
			if delta["type"] == "text_delta" {
				if text, ok := delta["text"].(string); ok {
					return text
				}
			}
		}
	case "result":
		// Final result message - extract from result.content
		if result, ok := msg["result"].(map[string]interface{}); ok {
			if content, ok := result["content"].([]interface{}); ok {
				var text strings.Builder
				for _, item := range content {
					if itemMap, ok := item.(map[string]interface{}); ok {
						if itemMap["type"] == "text" {
							if textContent, ok := itemMap["text"].(string); ok {
								text.WriteString(textContent)
							}
						}
					}
				}
				if text.Len() > 0 {
					return "\n" + text.String() + "\n"
				}
			}
		}
	}

	// For other message types (tool_use, etc.), return empty string
	// The raw output buffer still captures everything for TASK COMPLETE detection
	return ""
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
