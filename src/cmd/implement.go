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
  autom8 implement my-task -n 3`,
	Args: cobra.MaximumNArgs(1),
	RunE: runImplement,
}

func init() {
	ImplementCmd.Flags().IntVarP(&numInstances, "instances", "n", 1, "Number of parallel instances per task")
	ImplementCmd.Flags().IntVarP(&maxIterations, "max-iterations", "m", 0, "Maximum iterations per worktree (0 = unlimited)")
}

func loadAgentTemplate(name string) (string, error) {
	data, err := agentTemplates.ReadFile("agents/" + name + ".md")
	if err != nil {
		return "", err
	}
	return string(data), nil
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
					return fmt.Errorf("task '%s' is already ready (use 'autom8 converge' or 'autom8 accept')", targetTaskID)
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
	fmt.Printf("  %s %d\n", SubtitleStyle.Render("Instances per task:"), numInstances)
	fmt.Printf("  %s %d task(s) x %d = %d worktrees\n",
		SubtitleStyle.Render("Independent:"), len(independentTasks), numInstances, totalIndependent)
	if len(dependentTasks) > 0 {
		fmt.Printf("  %s %d task(s) x %d^2 = %d worktrees (exponential)\n",
			SubtitleStyle.Render("Dependent:"), len(dependentTasks), numInstances, totalDependent)
	}
	fmt.Println()

	// Mark all pending tasks as in-progress before starting
	for i, t := range tasks {
		for _, pt := range pendingTasks {
			if t.ID == pt.ID {
				tasks[i].Status = "in-progress"
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
		independentBranches[task.ID] = make([]string, numInstances)
		for i := 0; i < numInstances; i++ {
			suffix := fmt.Sprintf("-%d", i+1)
			independentBranches[task.ID][i] = suffix
			result := spawnWorkerForTask(task, gitRoot, worktreesDir, "", suffix, exePath, maxIterations)
			spawnedWorkers = append(spawnedWorkers, result)
		}
	}

	// Start dependent tasks
	for _, task := range dependentTasks {
		depSuffixes := independentBranches[task.DependsOn]
		if depSuffixes == nil {
			depSuffixes = make([]string, numInstances)
			for i := 0; i < numInstances; i++ {
				depSuffixes[i] = fmt.Sprintf("-%d", i+1)
			}
		}

		for _, depSuffix := range depSuffixes {
			for i := 0; i < numInstances; i++ {
				suffix := fmt.Sprintf("%s-%d", depSuffix, i+1)
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

	// Check if worktree already exists
	if _, err := os.Stat(worktreePath); err == nil {
		// Check if worker is already running (PID file is in logs directory, not worktree)
		autom8Path := filepath.Dir(worktreesDir)
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
	autom8Path := filepath.Dir(worktreesDir)
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
