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
	workerAutoAccept   bool
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
	WorkerCmd.Flags().BoolVar(&workerAutoAccept, "auto-accept", false, "Automatically accept the winning worktree after auto-converge")
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

	// Load configuration
	cfg, err := core.LoadConfig()
	if err != nil {
		return fmt.Errorf("error loading config: %w", err)
	}

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

	// Load the appropriate agent template based on task type
	templateName := "implementer"
	if task.GetType() == core.TaskTypeInvestigation {
		templateName = "investigator"
	}
	agentTemplate, err := loadAgentTemplate(templateName)
	if err != nil {
		agentTemplate = ""
	}

	// Build the prompt with agent template, task, and verification criteria
	var promptBuilder strings.Builder
	if agentTemplate != "" {
		promptBuilder.WriteString(agentTemplate)
	}
	// Inject worktree context so agents know where they are
	promptBuilder.WriteString(fmt.Sprintf("\n## Working Directory\n\nYour working directory is an isolated git worktree at: %s\nThis is where you should make all code changes and run commands.\nDo NOT confuse this with the main repository — your worktree is your workspace.\n\n", workerWorktreePath))
	// For investigation tasks, include the task ID so the agent knows the output path
	if task.GetType() == core.TaskTypeInvestigation {
		promptBuilder.WriteString(fmt.Sprintf("**Task ID:** %s\n\n", task.ID))
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

		// Run implementer tool; use PTY streaming for Claude to handle stream-json output.
		implTool := normalizeTool(cfg.Implementer.Tool)
		implCmd := buildImplementerCommand(cfg.Implementer, prompt, workerWorktreePath)

		var output []byte
		var err error
		if implTool == "claude" {
			output, err = runCommandWithPTYStreaming(implCmd, logFile)
		} else {
			output, err = runCommandWithStreaming(implCmd, logFile)
		}
		if err != nil {
			return fmt.Errorf("implementation iteration %d failed: %w", iteration, err)
		}

		// Check if output contains TASK COMPLETE signal
		if strings.Contains(string(output), "<output>TASK COMPLETE</output>") {
			// Implementation complete - now start the review loop
			reviewErr := runWorkerReviewLoop(task, workerWorktreePath, logsDir, workerBaseBranch, cfg.Reviewer)
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

			// Try auto-converge
			tryAutoConverge(workerTaskID, worktreeName, logsDir, workerAutoAccept)
			return nil // Success
		}

		// Continue to next iteration
	}
}

func normalizeTool(tool string) string {
	tool = strings.TrimSpace(strings.ToLower(tool))
	switch tool {
	case "claude", "codex", "opencode":
		return tool
	default:
		return "claude"
	}
}

// buildImplementerCommand creates the command to run the implementer tool.
func buildImplementerCommand(cfg core.ToolConfig, prompt, worktreePath string) *exec.Cmd {
	var cmd *exec.Cmd

	switch normalizeTool(cfg.Tool) {
	case "codex":
		args := []string{"exec", "--dangerously-bypass-approvals-and-sandbox"}
		if cfg.Model != "" {
			args = append(args, "--model", cfg.Model)
		}
		args = append(args, "-")
		cmd = exec.Command("codex", args...)
		cmd.Stdin = strings.NewReader(prompt)
	case "opencode":
		args := []string{"--yes", "--prompt", prompt}
		if cfg.Model != "" {
			args = append(args, "--model", cfg.Model)
		}
		cmd = exec.Command("opencode", args...)
	default: // "claude" or any other value defaults to claude
		args := []string{
			"-p", prompt,
			"--output-format", "stream-json",
			"--verbose",
			"--include-partial-messages",
			"--dangerously-skip-permissions",
		}
		if cfg.Model != "" {
			args = append(args, "--model", cfg.Model)
		}
		cmd = exec.Command("claude", args...)
	}

	cmd.Dir = worktreePath
	return cmd
}

// buildReviewerCommand creates the command to run the reviewer tool.
func buildReviewerCommand(cfg core.ToolConfig, prompt, worktreePath string) *exec.Cmd {
	var cmd *exec.Cmd

	switch normalizeTool(cfg.Tool) {
	case "claude":
		args := []string{
			"-p", prompt,
			"--dangerously-skip-permissions",
		}
		if cfg.Model != "" {
			args = append(args, "--model", cfg.Model)
		}
		cmd = exec.Command("claude", args...)
	case "opencode":
		args := []string{"--yes", "--prompt", prompt}
		if cfg.Model != "" {
			args = append(args, "--model", cfg.Model)
		}
		cmd = exec.Command("opencode", args...)
	case "codex":
		args := []string{"exec", "--dangerously-bypass-approvals-and-sandbox"}
		if cfg.Model != "" {
			args = append(args, "--model", cfg.Model)
		}
		args = append(args, "-")
		cmd = exec.Command("codex", args...)
		cmd.Stdin = strings.NewReader(prompt)
	default:
		args := []string{
			"-p", prompt,
			"--dangerously-skip-permissions",
		}
		if cfg.Model != "" {
			args = append(args, "--model", cfg.Model)
		}
		cmd = exec.Command("claude", args...)
	}

	cmd.Dir = worktreePath
	return cmd
}

// runWorkerReviewLoop runs the review loop after implementation completes.
// The reviewer agent will directly apply any fixes it finds.
func runWorkerReviewLoop(task core.Task, worktreePath, logsDir, baseBranch string, reviewerCfg core.ToolConfig) error {
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

		// Run reviewer tool with the review prompt
		reviewCmd := buildReviewerCommand(reviewerCfg, reviewPrompt, worktreePath)

		output, err := runCommandWithStreaming(reviewCmd, reviewLogFile)
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

	// Load the appropriate reviewer template based on task type
	templateName := "reviewer"
	if task.GetType() == core.TaskTypeInvestigation {
		templateName = "investigation-reviewer"
	}
	reviewerTemplate, err := loadAgentTemplate(templateName)
	if err != nil {
		return "", fmt.Errorf("failed to load reviewer template: %w", err)
	}

	sb.WriteString(reviewerTemplate)
	sb.WriteString("\n")
	// Inject worktree context so reviewers know where they are
	sb.WriteString(fmt.Sprintf("## Working Directory\n\nYour working directory is an isolated git worktree at: %s\nThis is where you should make all code changes and run commands.\nDo NOT confuse this with the main repository — your worktree is your workspace.\n\n", worktreePath))
	// For investigation tasks, include the task ID
	if task.GetType() == core.TaskTypeInvestigation {
		sb.WriteString(fmt.Sprintf("**Task ID:** %s\n\n", task.ID))
	}
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

	if task.GetType() == core.TaskTypeInvestigation {
		sb.WriteString("## Investigation Changes\n\n")
	} else {
		sb.WriteString("## Implementation Diff\n\n")
	}
	sb.WriteString("```diff\n")
	sb.WriteString(string(diffOutput))
	sb.WriteString("```\n")

	return sb.String(), nil
}

// tryAutoConverge checks if all sibling worktrees for a task are done and triggers convergence.
// This function handles race conditions via file locking to ensure only one worker runs converge.
// If autoAccept is true, the winning worktree is automatically accepted (merged) after convergence.
func tryAutoConverge(taskID, worktreeName, logsDir string, autoAccept bool) {
	// Open log file for appending converge output
	logFile := filepath.Join(logsDir, WorkerLogFile)
	logFd, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return // Can't log, just skip
	}
	defer logFd.Close()

	logMsg := func(format string, args ...interface{}) {
		fmt.Fprintf(logFd, "\n[auto-converge] "+format+"\n", args...)
	}

	// Get git root and worktrees directory
	gitRoot, err := core.GetGitRoot()
	if err != nil {
		logMsg("error: failed to get git root: %v", err)
		return
	}

	worktreesDir, err := core.GetWorktreesDir()
	if err != nil {
		logMsg("error: failed to get worktrees dir: %v", err)
		return
	}

	// Load tasks to get task IDs for matching
	tasks, err := core.LoadTasks()
	if err != nil {
		logMsg("error: failed to load tasks: %v", err)
		return
	}

	taskIDs := make(map[string]struct{})
	for _, t := range tasks {
		taskIDs[t.ID] = struct{}{}
	}

	// Find the task
	taskIndex := core.FindTaskIndex(tasks, taskID)
	if taskIndex == -1 {
		logMsg("error: task not found: %s", taskID)
		return
	}
	task := tasks[taskIndex]

	// Get all worktrees for this task
	pids, _ := core.LoadPids()
	worktreesByTask := core.ListWorktreesByTask(worktreesDir, taskIDs, pids)
	worktrees := worktreesByTask[taskID]

	if len(worktrees) == 0 {
		logMsg("no worktrees found for task %s", taskID)
		return
	}

	// Check if all worktrees are done by examining their Phase status.
	// We intentionally ignore IsRunning (PID-based check) because:
	// 1. Workers update status.json atomically before exiting
	// 2. PID checks can race when workers finish close together
	// 3. A worker that has set Phase=ready has completed all its work
	allDone := true
	for _, wt := range worktrees {
		// Terminal states: ready or idle (not implementing or reviewing)
		if wt.Phase == core.WorktreePhaseImplementing || wt.Phase == core.WorktreePhaseReviewing {
			allDone = false
			break
		}
	}

	if !allDone {
		logMsg("not all worktrees done yet, skipping auto-converge")
		return
	}

	logMsg("all %d worktrees done, attempting to acquire converge lock...", len(worktrees))

	// Try to acquire converge lock (file-based atomic operation)
	internalDir, err := core.GetInternalDir()
	if err != nil {
		logMsg("error: failed to get internal dir: %v", err)
		return
	}

	lockFile := filepath.Join(internalDir, fmt.Sprintf("converge-%s.lock", taskID))
	acquired, lockFd := acquireConvergeLock(lockFile)
	if !acquired {
		logMsg("another worker is handling converge, skipping")
		return
	}
	defer releaseConvergeLock(lockFile, lockFd)

	logMsg("acquired converge lock, running convergence...")

	// Filter to only ready worktrees for convergence
	var readyWorktrees []core.WorktreeInfo
	for _, wt := range worktrees {
		if wt.Phase == core.WorktreePhaseReady {
			readyWorktrees = append(readyWorktrees, wt)
		}
	}

	if len(readyWorktrees) == 0 {
		logMsg("no ready worktrees to converge")
		return
	}

	// Run convergence
	result := ConvergeTask(task, readyWorktrees, gitRoot)
	if result.Error != nil {
		logMsg("converge error: %v", result.Error)
		return
	}

	logMsg("winner: %s", result.Winner)
	if result.Reasoning != "" {
		logMsg("reasoning: %s", result.Reasoning)
	}

	// Update task with winner
	tasks[taskIndex].Winner = result.Winner
	if err := core.SaveTasks(tasks); err != nil {
		logMsg("error: failed to save tasks: %v", err)
		return
	}

	logMsg("auto-converge complete, winner saved to tasks.json")

	// Extract learnings and append to memory.md
	extractMemoryLearnings(task, readyWorktrees, result.Winner, gitRoot, logMsg)

	// Auto-accept the winning worktree if flag is set
	if autoAccept {
		logMsg("auto-accept enabled, accepting winner: %s", result.Winner)
		if err := AcceptWorktree(result.Winner, gitRoot); err != nil {
			logMsg("auto-accept error: %v", err)
			return
		}
		logMsg("auto-accept complete, winner merged to main branch")
	}
}

// acquireConvergeLock attempts to create an exclusive lock file.
// Returns true if the lock was acquired, along with the file descriptor.
func acquireConvergeLock(lockFile string) (bool, *os.File) {
	// Use O_EXCL to ensure atomic creation - only succeeds if file doesn't exist
	fd, err := os.OpenFile(lockFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return false, nil
	}
	// Write PID for debugging
	fmt.Fprintf(fd, "%d", os.Getpid())
	return true, fd
}

// releaseConvergeLock removes the lock file.
func releaseConvergeLock(lockFile string, fd *os.File) {
	if fd != nil {
		fd.Close()
	}
	os.Remove(lockFile)
}

// extractMemoryLearnings calls AI to extract general learnings from the completed task
// and appends them to .autom8/memory.md if valuable insights are found.
func extractMemoryLearnings(task core.Task, worktrees []core.WorktreeInfo, winner string, gitRoot string, logMsg func(string, ...interface{})) {
	logMsg("extracting learnings for memory.md...")

	// Find the winning worktree
	var winnerPath string
	for _, wt := range worktrees {
		if wt.Name == winner {
			winnerPath = wt.Path
			break
		}
	}

	if winnerPath == "" {
		logMsg("could not find winner worktree path")
		return
	}

	// Get the diff from the winning implementation
	diffCmd := exec.Command("git", "-C", winnerPath, "diff", "@{u}...HEAD")
	diffOutput, err := diffCmd.Output()
	if err != nil || len(diffOutput) == 0 {
		logMsg("could not get diff for learning extraction")
		return
	}

	// Build prompt for learning extraction
	prompt := buildMemoryExtractionPrompt(task, string(diffOutput))

	// Run claude to extract learnings
	claudeCmd := exec.Command("claude", "-p", "-", "--output-format", "json")
	claudeCmd.Dir = gitRoot
	claudeCmd.Stdin = strings.NewReader(prompt)

	output, err := claudeCmd.Output()
	if err != nil {
		logMsg("failed to run AI for learning extraction: %v", err)
		return
	}

	// Parse the response
	learning := parseMemoryLearningResponse(string(output))
	if learning == nil {
		logMsg("no valuable learnings extracted (this is fine)")
		return
	}

	// Append to memory.md
	autom8Dir, err := core.GetAutom8Dir()
	if err != nil {
		logMsg("failed to get autom8 dir: %v", err)
		return
	}

	memoryPath := filepath.Join(autom8Dir, "memory.md")
	appendLearningToMemory(memoryPath, learning, logMsg)
}

func buildMemoryExtractionPrompt(task core.Task, diff string) string {
	var sb strings.Builder

	sb.WriteString("You are analyzing a completed implementation task to extract general learnings.\n\n")
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

	sb.WriteString("## Winning Implementation Diff\n\n")
	sb.WriteString("```diff\n")
	// Truncate if too large
	if len(diff) > 30000 {
		diff = diff[:30000] + "\n... (truncated)"
	}
	sb.WriteString(diff)
	sb.WriteString("\n```\n\n")

	sb.WriteString("## Your Task\n\n")
	sb.WriteString("Extract any general learnings from this implementation that would be valuable for future tasks.\n\n")
	sb.WriteString("Guidelines:\n")
	sb.WriteString("- Focus on patterns, anti-patterns, or insights about this codebase\n")
	sb.WriteString("- Should be general principles, NOT task-specific details\n")
	sb.WriteString("- Keep it concise (1-3 sentences)\n")
	sb.WriteString("- If there's no meaningful learning worth persisting, output NONE\n\n")
	sb.WriteString("Output format:\n")
	sb.WriteString("<output>LEARNING: <brief topic> | <1-3 sentence general insight></output>\n")
	sb.WriteString("OR\n")
	sb.WriteString("<output>NONE</output>\n\n")
	sb.WriteString("The brief topic should be 2-4 words describing the category (e.g., \"PTY streaming\", \"Config loading\", \"Test patterns\").\n\n")
	sb.WriteString("Examples of good learnings:\n")
	sb.WriteString("- \"API conventions | Endpoints follow RESTful conventions with /api/v1 prefix. All handlers return JSON with {data, error} structure.\"\n")
	sb.WriteString("- \"Config loading | Configuration is loaded once at startup via core.LoadConfig(). Don't reload during request handling.\"\n")
	sb.WriteString("- \"Test patterns | Tests use table-driven pattern with subtests. Each test case should include name, input, and expected fields.\"\n\n")
	sb.WriteString("Examples of what NOT to add:\n")
	sb.WriteString("- Task-specific details like \"Added validation to the login form\"\n")
	sb.WriteString("- Obvious things like \"Functions should have descriptive names\"\n")
	sb.WriteString("- One-off fixes like \"Fixed typo in error message\"\n")

	return sb.String()
}

// memoryLearning holds the parsed topic and content from AI response.
type memoryLearning struct {
	Topic   string
	Content string
}

func parseMemoryLearningResponse(response string) *memoryLearning {
	// Try to parse JSON response first
	var jsonResp struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal([]byte(response), &jsonResp); err == nil {
		response = jsonResp.Result
	}

	// Check for NONE output
	if strings.Contains(response, "<output>NONE</output>") {
		return nil
	}

	// Look for "<output>LEARNING: topic | content</output>" pattern
	if start := strings.Index(response, "<output>LEARNING:"); start != -1 {
		start += len("<output>LEARNING:")
		if end := strings.Index(response[start:], "</output>"); end != -1 {
			learningText := strings.TrimSpace(response[start : start+end])
			// Split by | to get topic and content
			if idx := strings.Index(learningText, "|"); idx != -1 {
				return &memoryLearning{
					Topic:   strings.TrimSpace(learningText[:idx]),
					Content: strings.TrimSpace(learningText[idx+1:]),
				}
			}
			// Fallback: no separator, use "General" as topic
			return &memoryLearning{
				Topic:   "General",
				Content: learningText,
			}
		}
	}

	return nil
}

func appendLearningToMemory(memoryPath string, learning *memoryLearning, logMsg func(string, ...interface{})) {
	// Create the file if it doesn't exist with header
	if _, err := os.Stat(memoryPath); os.IsNotExist(err) {
		header := `# Agent Memory

This file stores persistent learnings that agents can reference across sessions.

---

`
		if err := os.WriteFile(memoryPath, []byte(header), 0644); err != nil {
			logMsg("failed to create memory.md: %v", err)
			return
		}
	}

	// Format the entry: ## <date>: <brief topic>
	date := time.Now().Format("2006-01-02")
	entry := fmt.Sprintf("\n## %s: %s\n%s\n", date, learning.Topic, learning.Content)

	// Append to file
	f, err := os.OpenFile(memoryPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		logMsg("failed to open memory.md: %v", err)
		return
	}
	defer f.Close()

	if _, err := f.WriteString(entry); err != nil {
		logMsg("failed to write to memory.md: %v", err)
		return
	}

	logMsg("learning appended to memory.md")
}
