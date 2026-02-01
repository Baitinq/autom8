package cmd

import (
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/baitinq/autom8/src/core"
	"github.com/spf13/cobra"
)

//go:embed agents/*.md
var agentTemplates embed.FS

const (
	workerLogFile = "worker.log"
)

var (
	numInstances  int
	maxIterations int
	addMode       bool
)

var ImplementCmd = &cobra.Command{
	Use:   "implement [task-name]",
	Short: "Implement pending tasks using AI",
	Long: `Launch Claude AI agents to implement pending tasks.

If a task name is provided, only that task will be implemented.
Otherwise, all pending tasks will be implemented.

Each agent runs in an isolated git worktree, allowing multiple parallel
implementations without conflicts. For dependent tasks, the branching
is exponential - each instance of a dependent task branches from each
instance of its parent task.

The command spawns worker subprocesses for each worktree and returns
immediately. Use 'autom8 status' to monitor progress.`,
	Example: `  # Implement all pending tasks
  autom8 implement

  # Implement a specific task
  autom8 implement my-task

  # Multiple parallel implementations
  autom8 implement -n 3
  autom8 implement my-task -n 3

  # Add more implementations to an existing task
  autom8 implement my-task -n 2 --add`,
	Args: cobra.MaximumNArgs(1),
	RunE: runImplement,
}

func init() {
	ImplementCmd.Flags().IntVarP(&numInstances, "instances", "n", 1, "Number of parallel instances per task")
	ImplementCmd.Flags().IntVarP(&maxIterations, "max-iterations", "m", 0, "Maximum iterations per worktree (0 = unlimited)")
	ImplementCmd.Flags().BoolVar(&addMode, "add", false, "Add more implementations to an existing task (allows ready/in-progress tasks)")
}

func loadAgentTemplate(name string) (string, error) {
	data, err := agentTemplates.ReadFile("agents/" + name + ".md")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// findHighestInstanceNumber scans the worktrees directory to find the highest instance number
// for a given task ID. Returns 0 if no instances exist.
func findHighestInstanceNumber(worktreesDir, taskID string) int {
	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		return 0
	}

	maxInstance := 0
	prefix := taskID + "-"

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}

		// Extract instance number from the suffix (e.g., "my-task-3" -> 3)
		suffix := strings.TrimPrefix(name, prefix)
		// Handle simple case: just a number (e.g., "3")
		if num, err := strconv.Atoi(suffix); err == nil {
			if num > maxInstance {
				maxInstance = num
			}
			continue
		}
		// Handle dependent case: first number before another dash (e.g., "2-1" means instance 2)
		// This is for dependent tasks that have suffixes like "-1-2" (parent instance - child instance)
		// We want the first number after the task ID
		parts := strings.SplitN(suffix, "-", 2)
		if len(parts) > 0 {
			if num, err := strconv.Atoi(parts[0]); err == nil {
				if num > maxInstance {
					maxInstance = num
				}
			}
		}
	}

	return maxInstance
}

func findParentSuffixes(worktreesDir, taskID string, taskIDs map[string]struct{}) []string {
	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		return nil
	}

	prefix := taskID + "-"
	suffixes := []string{}
	seen := make(map[string]struct{})

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		baseID, ok := core.BaseTaskIDFromWorktree(name, taskIDs)
		if !ok || baseID != taskID {
			continue
		}
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		remainder := strings.TrimPrefix(name, prefix)
		if remainder == "" {
			continue
		}
		parts := strings.SplitN(remainder, "-", 2)
		if len(parts) == 0 || parts[0] == "" {
			continue
		}
		if _, err := strconv.Atoi(parts[0]); err != nil {
			continue
		}
		suffix := "-" + remainder
		if _, ok := seen[suffix]; ok {
			continue
		}
		seen[suffix] = struct{}{}
		suffixes = append(suffixes, suffix)
	}

	return suffixes
}

func findHighestChildInstance(worktreesDir, taskID, depSuffix string) int {
	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		return 0
	}

	maxInstance := 0
	prefix := taskID + depSuffix + "-"

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		childSuffix := strings.TrimPrefix(name, prefix)
		parts := strings.SplitN(childSuffix, "-", 2)
		if num, err := strconv.Atoi(parts[0]); err == nil {
			if num > maxInstance {
				maxInstance = num
			}
		}
	}

	return maxInstance
}

func runImplement(cmd *cobra.Command, args []string) error {
	// Check git repo first
	if _, err := core.GetGitRoot(); err != nil {
		return err
	}

	if numInstances < 1 {
		numInstances = 1
	}

	// Check if a specific task ID was provided
	var targetTaskID string
	if len(args) > 0 {
		targetTaskID = args[0]
	}

	tasks, err := core.LoadTasks()
	if err != nil {
		return fmt.Errorf("error loading tasks: %w", err)
	}

	if len(tasks) == 0 {
		fmt.Println(SubtitleStyle.Render("No tasks found. Use 'autom8 new' to create one."))
		return nil
	}

	// Filter tasks to implement
	var pendingTasks []core.Task
	for _, task := range tasks {
		// If a specific task ID was provided, only include that task
		if targetTaskID != "" {
			if task.ID == targetTaskID {
				if task.Status == "completed" {
					return fmt.Errorf("task '%s' is already completed", targetTaskID)
				}
				if task.Status == "ready" {
					if !addMode {
						return fmt.Errorf("task '%s' is already ready (use 'autom8 converge' or 'autom8 accept', or use --add to add more implementations)", targetTaskID)
					}
					// In add mode, allow ready tasks
				} else if task.Status == "in-progress" && !addMode {
					// For in-progress without --add, it's allowed (continue existing behavior)
				}
				pendingTasks = append(pendingTasks, task)
				break
			}
		} else if task.Status == "pending" {
			pendingTasks = append(pendingTasks, task)
		}
	}

	if targetTaskID != "" && len(pendingTasks) == 0 {
		return fmt.Errorf("task '%s' not found", targetTaskID)
	}

	if len(pendingTasks) == 0 {
		fmt.Println(SubtitleStyle.Render("No pending tasks to implement."))
		return nil
	}

	gitRoot, err := core.GetGitRoot()
	if err != nil {
		return err
	}

	autom8Path, err := core.EnsureAutom8Dir()
	if err != nil {
		return fmt.Errorf("error ensuring autom8 dir: %w", err)
	}

	worktreesDir := filepath.Join(autom8Path, "worktrees")
	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		return fmt.Errorf("error creating worktrees dir: %w", err)
	}

	// Build task map for dependency lookup
	taskMap := make(map[string]core.Task)
	for _, t := range tasks {
		taskMap[t.ID] = t
	}
	taskIDs := make(map[string]struct{}, len(tasks))
	for _, t := range tasks {
		taskIDs[t.ID] = struct{}{}
	}

	// Separate tasks with and without dependencies
	var independentTasks []core.Task
	var dependentTasks []core.Task
	for _, task := range pendingTasks {
		if task.DependsOn == "" {
			independentTasks = append(independentTasks, task)
		} else {
			dependentTasks = append(dependentTasks, task)
		}
	}

	// Calculate total instances (exponential for dependencies)
	totalIndependent := len(independentTasks) * numInstances
	totalDependent := len(dependentTasks) * numInstances * numInstances

	fmt.Println(TitleStyle.Render("Starting Implementation"))
	fmt.Println()
	if addMode {
		fmt.Printf("  %s adding %d more implementation(s)\n", SubtitleStyle.Render("Mode:"), numInstances)
	} else {
		fmt.Printf("  %s %d\n", SubtitleStyle.Render("Instances per task:"), numInstances)
	}
	fmt.Printf("  %s %d task(s) x %d = %d worktrees\n",
		SubtitleStyle.Render("Independent:"), len(independentTasks), numInstances, totalIndependent)
	if len(dependentTasks) > 0 {
		fmt.Printf("  %s %d task(s) x %d^2 = %d worktrees (exponential)\n",
			SubtitleStyle.Render("Dependent:"), len(dependentTasks), numInstances, totalDependent)
	}
	fmt.Println()

	// Mark all pending tasks as in-progress before starting
	// In add mode for ready tasks, also clear the Winner field
	for i, t := range tasks {
		for _, pt := range pendingTasks {
			if t.ID == pt.ID {
				if addMode && t.Status == "ready" {
					// Reset ready task to in-progress and clear winner
					tasks[i].Status = "in-progress"
					tasks[i].Winner = ""
				} else if t.Status == "pending" {
					tasks[i].Status = "in-progress"
				}
				// For in-progress tasks, no change needed
				break
			}
		}
	}
	if err := core.SaveTasks(tasks); err != nil {
		return fmt.Errorf("error updating task status: %w", err)
	}

	// Get the path to the current executable for spawning workers
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Track spawned workers
	var spawnedWorkers []string

	// Track created branches for independent tasks
	independentBranches := make(map[string][]string)

	// Start independent tasks
	for _, task := range independentTasks {
		// Determine starting instance number
		startInstance := 1
		if addMode {
			maxExisting := findHighestInstanceNumber(worktreesDir, task.ID)
			startInstance = maxExisting + 1
		}

		independentBranches[task.ID] = make([]string, numInstances)
		for i := 0; i < numInstances; i++ {
			instanceNum := startInstance + i
			suffix := fmt.Sprintf("-%d", instanceNum)
			independentBranches[task.ID][i] = suffix
			result := spawnWorkerForTask(task, gitRoot, worktreesDir, "", suffix, exePath, maxIterations)
			spawnedWorkers = append(spawnedWorkers, result)
		}
	}

	// Start dependent tasks
	for _, task := range dependentTasks {
		depSuffixes := independentBranches[task.DependsOn]
		if depSuffixes == nil {
			if addMode {
				depSuffixes = findParentSuffixes(worktreesDir, task.DependsOn, taskIDs)
			}
			if len(depSuffixes) == 0 {
				depSuffixes = make([]string, numInstances)
				for i := 0; i < numInstances; i++ {
					depSuffixes[i] = fmt.Sprintf("-%d", i+1)
				}
			}
		}

		for _, depSuffix := range depSuffixes {
			startInstance := 1
			if addMode {
				maxExisting := findHighestChildInstance(worktreesDir, task.ID, depSuffix)
				startInstance = maxExisting + 1
			}
			for i := 0; i < numInstances; i++ {
				instanceNum := startInstance + i
				suffix := fmt.Sprintf("%s-%d", depSuffix, instanceNum)
				baseBranch := fmt.Sprintf("%s%s", task.DependsOn, depSuffix)
				result := spawnWorkerForTask(task, gitRoot, worktreesDir, baseBranch, suffix, exePath, maxIterations)
				spawnedWorkers = append(spawnedWorkers, result)
			}
		}
	}

	// Print results
	for _, result := range spawnedWorkers {
		fmt.Println(result)
	}

	fmt.Println()
	fmt.Println(SuccessStyle.Render("Workers spawned!"))
	fmt.Println(SubtitleStyle.Render("Use 'autom8 status' to monitor progress."))
	return nil
}

// spawnWorkerForTask creates a worktree and spawns a detached worker subprocess
func spawnWorkerForTask(task core.Task, gitRoot, worktreesDir, baseBranchID, suffix, exePath string, maxIter int) string {
	instanceID := task.ID + suffix
	worktreePath := filepath.Join(worktreesDir, instanceID)

	branchName := fmt.Sprintf("autom8/%s", instanceID)
	autom8Path := filepath.Dir(worktreesDir)

	// Check if worktree already exists
	if _, err := os.Stat(worktreePath); err == nil {
		// Check if worker is already running (PID file is in logs directory, not worktree)
		pidFile := filepath.Join(autom8Path, "logs", instanceID, core.WorkerPidFile)
		if pidData, err := os.ReadFile(pidFile); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(pidData))); err == nil {
				if core.IsProcessRunning(pid) {
					return fmt.Sprintf("  %s %s (worker already running, PID %d)", SubtitleStyle.Render("[skip]"), instanceID, pid)
				}
			}
		}
		return fmt.Sprintf("  %s %s (already exists, no active worker)", SubtitleStyle.Render("[skip]"), instanceID)
	}

	// Clear any stale status file for a previous worktree with the same name.
	statusPath := filepath.Join(autom8Path, "logs", instanceID, core.WorktreeStatusFile)
	if err := os.Remove(statusPath); err != nil && !os.IsNotExist(err) {
		return fmt.Sprintf("  %s %s: failed to clear stale status: %v", ErrorStyle.Render("[error]"), instanceID, err)
	}

	// Determine base branch for worktree creation and review
	var baseBranch string
	var cmd *exec.Cmd
	if baseBranchID != "" {
		baseBranch = fmt.Sprintf("autom8/%s", baseBranchID)
		cmd = exec.Command("git", "-C", gitRoot, "worktree", "add", "-b", branchName, worktreePath, baseBranch)
	} else {
		baseBranch = "main"
		cmd = exec.Command("git", "-C", gitRoot, "worktree", "add", "-b", branchName, worktreePath)
	}

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Sprintf("  %s %s: %v\n%s", ErrorStyle.Render("[error]"), instanceID, err, string(output))
	}

	// Create logs directory for this worktree
	logsDir := filepath.Join(autom8Path, "logs", instanceID)
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return fmt.Sprintf("  %s %s: failed to create logs dir: %v", ErrorStyle.Render("[error]"), instanceID, err)
	}

	// Spawn a detached worker subprocess
	workerArgs := []string{
		"_worker",
		"--worktree-path", worktreePath,
		"--base-branch", baseBranch,
		"--task-id", task.ID,
	}
	if maxIter > 0 {
		workerArgs = append(workerArgs, "--max-iterations", strconv.Itoa(maxIter))
	}

	workerCmd := exec.Command(exePath, workerArgs...)

	// Redirect stdout/stderr to log file
	logFile := filepath.Join(logsDir, workerLogFile)
	logFd, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Sprintf("  %s %s: failed to create log file: %v", ErrorStyle.Render("[error]"), instanceID, err)
	}
	workerCmd.Stdout = logFd
	workerCmd.Stderr = logFd

	// Set up process attributes for detachment (setsid equivalent)
	workerCmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	// Start the worker
	if err := workerCmd.Start(); err != nil {
		logFd.Close()
		return fmt.Sprintf("  %s %s: failed to start worker: %v", ErrorStyle.Render("[error]"), instanceID, err)
	}

	// Close log file descriptor (worker has its own reference now)
	logFd.Close()

	pid := workerCmd.Process.Pid
	return fmt.Sprintf("  %s %s (PID %d, branch: %s)", SuccessStyle.Render("[spawned]"), instanceID, pid, HighlightStyle.Render(branchName))
}
