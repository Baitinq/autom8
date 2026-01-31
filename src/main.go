package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

const (
	autom8Dir = ".autom8"
	tasksFile = "tasks.json"
	pidsFile  = "pids.json"
)

// Styles for terminal output
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205"))

	subtitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	statusPendingStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("214")).
				Bold(true)

	statusInProgressStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("33")).
				Bold(true)

	statusCompletedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("42")).
				Bold(true)

	idStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("245"))

	highlightStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("99"))
)

type Task struct {
	ID                   string    `json:"id"`
	Prompt               string    `json:"prompt"`
	VerificationCriteria []string  `json:"verification_criteria"`
	DependsOn            string    `json:"depends_on,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
	Status               string    `json:"status"`
}

var rootCmd = &cobra.Command{
	Use:   "autom8",
	Short: "Automate AI agent workflows",
	Long: `autom8 is a CLI tool that orchestrates AI-driven development workflows.

It enables you to:
  - Define implementation tasks with verification criteria
  - Manage task dependencies
  - Run multiple Claude AI agents in parallel
  - Isolate each agent's work in separate git worktrees`,
	SilenceUsage: true,
}

var featureCmd = &cobra.Command{
	Use:   "feature",
	Short: "Create a new task/prompt",
	Long: `Create a new task with a prompt and optional verification criteria.

Without flags, starts an interactive mode to guide you through task creation.
With flags, creates the task directly (non-interactive mode).`,
	Example: `  # Interactive mode
  autom8 feature

  # Non-interactive mode
  autom8 feature -p "Add login page" -c "Has email field" -c "Has password field"

  # With dependency
  autom8 feature -p "Add logout button" -d task-123456789`,
	RunE: runFeature,
}

var implementCmd = &cobra.Command{
	Use:   "implement [task-id]",
	Short: "Implement pending tasks using AI",
	Long: `Launch Claude AI agents to implement pending tasks.

If a task ID is provided, only that task will be implemented.
Otherwise, all pending tasks will be implemented.

Each agent runs in an isolated git worktree, allowing multiple parallel
implementations without conflicts. For dependent tasks, the branching
is exponential - each instance of a dependent task branches from each
instance of its parent task.`,
	Example: `  # Implement all pending tasks
  autom8 implement

  # Implement a specific task
  autom8 implement task-123456789

  # Multiple parallel implementations
  autom8 implement -n 3
  autom8 implement task-123456789 -n 3`,
	Args: cobra.MaximumNArgs(1),
	RunE: runImplement,
}

var statusCmd = &cobra.Command{
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

var acceptCmd = &cobra.Command{
	Use:   "accept <worktree-name>",
	Short: "Merge a worktree branch into current branch and clean up",
	Long: `Accept and merge a completed implementation from a worktree.

This command will:
  1. Auto-commit any uncommitted changes in the worktree
  2. Merge the worktree's branch into your current branch
  3. Remove the worktree directory
  4. Delete the merged branch`,
	Example: `  autom8 accept task-123456789-1`,
	Args:    cobra.ExactArgs(1),
	RunE:    runAccept,
}

var deleteCmd = &cobra.Command{
	Use:     "delete <task-id>",
	Aliases: []string{"rm"},
	Short:   "Delete a task by ID",
	Long: `Delete a task from the task list.

Note: Tasks that have other tasks depending on them cannot be deleted
until their dependents are deleted first.`,
	Example: `  autom8 delete task-123456789`,
	Args:    cobra.ExactArgs(1),
	RunE:    runDelete,
}

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion scripts",
	Long: `Generate completion scripts for your shell.

To load completions:

Bash:
  $ source <(autom8 completion bash)
  # To load completions for each session, execute once:
  # Linux:
  $ autom8 completion bash > /etc/bash_completion.d/autom8
  # macOS:
  $ autom8 completion bash > $(brew --prefix)/etc/bash_completion.d/autom8

Zsh:
  # If shell completion is not already enabled in your environment,
  # you will need to enable it. You can execute the following once:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ autom8 completion zsh > "${fpath[1]}/_autom8"

Fish:
  $ autom8 completion fish | source
  # To load completions for each session, execute once:
  $ autom8 completion fish > ~/.config/fish/completions/autom8.fish

PowerShell:
  PS> autom8 completion powershell | Out-String | Invoke-Expression
  # To load completions for every new session, run:
  PS> autom8 completion powershell > autom8.ps1
  # and source this file from your PowerShell profile.
`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return rootCmd.GenBashCompletion(os.Stdout)
		case "zsh":
			return rootCmd.GenZshCompletion(os.Stdout)
		case "fish":
			return rootCmd.GenFishCompletion(os.Stdout, true)
		case "powershell":
			return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
		default:
			return fmt.Errorf("unknown shell: %s", args[0])
		}
	},
}

// Flags
var (
	promptFlag    string
	criteriaFlags []string
	dependsOnFlag string
	numInstances  int
)

func init() {
	rootCmd.AddCommand(featureCmd)
	rootCmd.AddCommand(implementCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(acceptCmd)
	rootCmd.AddCommand(deleteCmd)
	rootCmd.AddCommand(completionCmd)

	// Feature command flags
	featureCmd.Flags().StringVarP(&promptFlag, "prompt", "p", "", "Task prompt (non-interactive mode)")
	featureCmd.Flags().StringArrayVarP(&criteriaFlags, "criteria", "c", []string{}, "Verification criteria (can be specified multiple times)")
	featureCmd.Flags().StringVarP(&dependsOnFlag, "depends-on", "d", "", "Task ID this depends on")

	// Implement command flags
	implementCmd.Flags().IntVarP(&numInstances, "instances", "n", 1, "Number of parallel instances per task")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func getGitRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("must be run inside a git repository")
	}
	return strings.TrimSpace(string(output)), nil
}

func getAutom8Dir() (string, error) {
	gitRoot, err := getGitRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(gitRoot, autom8Dir), nil
}

func ensureAutom8Dir() (string, error) {
	dir, err := getAutom8Dir()
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	return dir, nil
}

func loadTasks() ([]Task, error) {
	dir, err := getAutom8Dir()
	if err != nil {
		return nil, err
	}

	tasksPath := filepath.Join(dir, tasksFile)

	data, err := os.ReadFile(tasksPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []Task{}, nil
		}
		return nil, err
	}

	var tasks []Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil, err
	}

	return tasks, nil
}

func saveTasks(tasks []Task) error {
	dir, err := ensureAutom8Dir()
	if err != nil {
		return err
	}

	tasksPath := filepath.Join(dir, tasksFile)

	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(tasksPath, data, 0644)
}

// PID tracking for worktrees
func loadPids() (map[string]int, error) {
	dir, err := getAutom8Dir()
	if err != nil {
		return make(map[string]int), nil
	}

	pidsPath := filepath.Join(dir, pidsFile)
	data, err := os.ReadFile(pidsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]int), nil
		}
		return nil, err
	}

	var pids map[string]int
	if err := json.Unmarshal(data, &pids); err != nil {
		return make(map[string]int), nil
	}
	return pids, nil
}

func savePids(pids map[string]int) error {
	dir, err := ensureAutom8Dir()
	if err != nil {
		return err
	}

	pidsPath := filepath.Join(dir, pidsFile)
	data, err := json.MarshalIndent(pids, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(pidsPath, data, 0644)
}

func savePid(worktreeName string, pid int) {
	pids, _ := loadPids()
	pids[worktreeName] = pid
	savePids(pids)
}

func isProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds, so we need to send signal 0 to check
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

func runFeature(cmd *cobra.Command, args []string) error {
	// Check git repo first
	if _, err := getGitRoot(); err != nil {
		return err
	}

	var prompt string
	var criteria []string
	var dependsOn string

	if promptFlag != "" {
		// Non-interactive mode
		prompt = promptFlag
		criteria = criteriaFlags
		dependsOn = dependsOnFlag
	} else {
		// Interactive mode with huh
		var criteriaInput string

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewText().
					Title("Task Prompt").
					Description("What should the AI implement?").
					Placeholder("Add a login page with email and password fields...").
					Value(&prompt).
					Validate(func(s string) error {
						if strings.TrimSpace(s) == "" {
							return fmt.Errorf("prompt cannot be empty")
						}
						return nil
					}),
			),
			huh.NewGroup(
				huh.NewText().
					Title("Verification Criteria").
					Description("How should success be verified? (one per line, optional)").
					Placeholder("Has email field\nHas password field\nValidates input").
					Value(&criteriaInput),
			),
			huh.NewGroup(
				huh.NewInput().
					Title("Depends On").
					Description("Task ID this depends on (optional)").
					Placeholder("task-123456789").
					Value(&dependsOn),
			),
		).WithTheme(huh.ThemeDracula())

		err := form.Run()
		if err != nil {
			if err == huh.ErrUserAborted {
				fmt.Println("\nAborted.")
				return nil
			}
			return err
		}

		// Parse criteria from multiline input
		if strings.TrimSpace(criteriaInput) != "" {
			for _, line := range strings.Split(criteriaInput, "\n") {
				line = strings.TrimSpace(line)
				if line != "" {
					criteria = append(criteria, line)
				}
			}
		}
	}

	if strings.TrimSpace(prompt) == "" {
		return fmt.Errorf("no prompt provided")
	}

	tasks, err := loadTasks()
	if err != nil {
		return fmt.Errorf("error loading tasks: %w", err)
	}

	// Validate dependency exists if specified
	if dependsOn != "" {
		found := false
		for _, t := range tasks {
			if t.ID == dependsOn {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("dependency task '%s' not found", dependsOn)
		}
	}

	task := Task{
		ID:                   fmt.Sprintf("task-%d", time.Now().UnixNano()),
		Prompt:               prompt,
		VerificationCriteria: criteria,
		DependsOn:            dependsOn,
		CreatedAt:            time.Now(),
		Status:               "pending",
	}

	tasks = append(tasks, task)

	if err := saveTasks(tasks); err != nil {
		return fmt.Errorf("error saving task: %w", err)
	}

	fmt.Println()
	fmt.Println(successStyle.Render("Task created successfully!"))
	fmt.Printf("  %s %s\n", subtitleStyle.Render("ID:"), idStyle.Render(task.ID))
	return nil
}

// WorktreeInfo holds information about a worktree's status
type WorktreeInfo struct {
	Name         string
	Path         string
	Branch       string
	CommitsAhead string
	HasChanges   bool
	IsRunning    bool
}

func getWorktreeInfo(worktreesDir, worktreeName string, pids map[string]int) WorktreeInfo {
	worktreePath := filepath.Join(worktreesDir, worktreeName)
	info := WorktreeInfo{
		Name: worktreeName,
		Path: worktreePath,
	}

	// Get the branch name
	branchCmd := exec.Command("git", "-C", worktreePath, "branch", "--show-current")
	if branchOutput, err := branchCmd.Output(); err == nil {
		info.Branch = strings.TrimSpace(string(branchOutput))
	} else {
		info.Branch = "unknown"
	}

	// Check if there are any git changes
	statusCmd := exec.Command("git", "-C", worktreePath, "status", "--porcelain")
	if statusOutput, err := statusCmd.Output(); err == nil {
		info.HasChanges = len(strings.TrimSpace(string(statusOutput))) > 0
	}

	// Check how many commits are ahead
	aheadCmd := exec.Command("git", "-C", worktreePath, "rev-list", "--count", "HEAD", "^main")
	if aheadOutput, err := aheadCmd.Output(); err == nil {
		info.CommitsAhead = strings.TrimSpace(string(aheadOutput))
	} else {
		info.CommitsAhead = "0"
	}

	// Check if the tracked process is still running
	if pid, ok := pids[worktreeName]; ok {
		info.IsRunning = isProcessRunning(pid)
	}

	return info
}

func runStatus(cmd *cobra.Command, args []string) error {
	if _, err := getGitRoot(); err != nil {
		return err
	}

	tasks, err := loadTasks()
	if err != nil {
		return fmt.Errorf("error loading tasks: %w", err)
	}

	// Get worktrees and PIDs
	autom8Path, _ := getAutom8Dir()
	worktreesDir := filepath.Join(autom8Path, "worktrees")
	worktreesByTask := make(map[string][]WorktreeInfo)
	pids, _ := loadPids()

	if entries, err := os.ReadDir(worktreesDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			worktreeName := entry.Name()
			// Extract task ID: task-{timestamp}-{instance} -> task-{timestamp}
			taskID := worktreeName
			if lastDash := strings.LastIndex(worktreeName, "-"); lastDash > 0 {
				taskID = worktreeName[:lastDash]
			}
			info := getWorktreeInfo(worktreesDir, worktreeName, pids)
			worktreesByTask[taskID] = append(worktreesByTask[taskID], info)
		}
	}

	if len(tasks) == 0 {
		fmt.Println(subtitleStyle.Render("No tasks found. Use 'autom8 feature' to create one."))
		return nil
	}

	// Build dependency tree
	taskMap := make(map[string]Task)
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

	fmt.Println(titleStyle.Render("Status"))
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
			statusBadge = statusPendingStyle.Render("[pending]")
		case "in-progress":
			statusBadge = statusInProgressStyle.Render("[in-progress]")
		case "completed":
			statusBadge = statusCompletedStyle.Render("[completed]")
		default:
			statusBadge = subtitleStyle.Render(fmt.Sprintf("[%s]", task.Status))
		}

		// Print task header
		fmt.Printf("%s%s%s %s\n", prefix, branch, statusBadge, truncate(task.Prompt, 50))
		fmt.Printf("%s%s %s\n", childPrefix, subtitleStyle.Render("ID:"), idStyle.Render(task.ID))

		// Print verification criteria
		if len(task.VerificationCriteria) > 0 {
			fmt.Printf("%s%s\n", childPrefix, subtitleStyle.Render("Criteria:"))
			for _, c := range task.VerificationCriteria {
				fmt.Printf("%s  • %s\n", childPrefix, c)
			}
		}

		// Print worktrees for this task
		worktrees := worktreesByTask[task.ID]
		children := childrenMap[task.ID]
		hasMore := len(children) > 0

		if len(worktrees) > 0 {
			fmt.Printf("%s%s\n", childPrefix, subtitleStyle.Render("Worktrees:"))
			for i, wt := range worktrees {
				wtIsLast := i == len(worktrees)-1 && !hasMore
				wtBranch := "├── "
				if wtIsLast {
					wtBranch = "└── "
				}

				// Worktree status
				var wtStatus string
				if wt.IsRunning {
					wtStatus = statusInProgressStyle.Render("[running]")
				} else if wt.HasChanges {
					wtStatus = statusPendingStyle.Render("[modified]")
				} else if wt.CommitsAhead != "0" {
					wtStatus = statusCompletedStyle.Render("[" + wt.CommitsAhead + " commits]")
				} else {
					wtStatus = subtitleStyle.Render("[idle]")
				}

				fmt.Printf("%s%s%s %s\n", childPrefix, wtBranch, wtStatus, wt.Name)

				// Show accept hint
				if !wt.IsRunning && (wt.CommitsAhead != "0" || wt.HasChanges) {
					wtChildPrefix := childPrefix + "│   "
					if wtIsLast {
						wtChildPrefix = childPrefix + "    "
					}
					fmt.Printf("%s%s autom8 accept %s\n", wtChildPrefix, highlightStyle.Render("→"), wt.Name)
				}
			}
		} else if task.Status == "pending" {
			fmt.Printf("%s%s\n", childPrefix, subtitleStyle.Render("(no worktrees - run 'autom8 implement')"))
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

func runAccept(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("worktree name required\nRun 'autom8 status' to see available worktrees")
	}

	worktreeName := args[0]

	gitRoot, err := getGitRoot()
	if err != nil {
		return fmt.Errorf("error getting git root: %w", err)
	}

	autom8Path, err := getAutom8Dir()
	if err != nil {
		return fmt.Errorf("error getting autom8 dir: %w", err)
	}

	worktreePath := filepath.Join(autom8Path, "worktrees", worktreeName)

	// Check if worktree exists
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		return fmt.Errorf("worktree '%s' not found\nRun 'autom8 status' to see available worktrees", worktreeName)
	}

	// Get the branch name from the worktree
	branchCmd := exec.Command("git", "-C", worktreePath, "branch", "--show-current")
	branchOutput, err := branchCmd.Output()
	if err != nil {
		return fmt.Errorf("error getting branch name: %w", err)
	}
	branchName := strings.TrimSpace(string(branchOutput))

	if branchName == "" {
		return fmt.Errorf("could not determine branch name for worktree")
	}

	// Check for uncommitted changes in the worktree
	statusCmd := exec.Command("git", "-C", worktreePath, "status", "--porcelain")
	statusOutput, err := statusCmd.Output()
	if err != nil {
		return fmt.Errorf("error checking worktree status: %w", err)
	}

	if len(strings.TrimSpace(string(statusOutput))) > 0 {
		fmt.Println(subtitleStyle.Render("Found uncommitted changes, auto-committing..."))

		// Stage all changes
		addCmd := exec.Command("git", "-C", worktreePath, "add", "-A")
		if addOutput, err := addCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("error staging changes: %w\n%s", err, string(addOutput))
		}

		// Commit with auto-commit message
		commitCmd := exec.Command("git", "-C", worktreePath, "commit", "-m", "autom8: auto-commit uncommitted changes")
		if commitOutput, err := commitCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("error committing changes: %w\n%s", err, string(commitOutput))
		}
		fmt.Println(successStyle.Render("Auto-committed successfully."))
	}

	fmt.Printf("Merging branch '%s' into current branch...\n", highlightStyle.Render(branchName))

	// Merge the branch into the current branch
	mergeCmd := exec.Command("git", "-C", gitRoot, "merge", branchName, "-m", fmt.Sprintf("Merge %s (autom8 accept)", branchName))
	mergeOutput, err := mergeCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error merging branch: %w\n%s\nResolve conflicts manually, then run 'autom8 accept' again to clean up", err, string(mergeOutput))
	}
	fmt.Printf("%s", string(mergeOutput))

	// Remove the worktree
	fmt.Printf("Removing worktree '%s'...\n", worktreeName)
	removeCmd := exec.Command("git", "-C", gitRoot, "worktree", "remove", worktreePath)
	removeOutput, err := removeCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error removing worktree: %w\n%s\nYou may need to manually remove it with: git worktree remove %s", err, string(removeOutput), worktreePath)
	}

	// Delete the branch (it's been merged)
	fmt.Printf("Deleting branch '%s'...\n", branchName)
	deleteBranchCmd := exec.Command("git", "-C", gitRoot, "branch", "-d", branchName)
	deleteBranchOutput, err := deleteBranchCmd.CombinedOutput()
	if err != nil {
		fmt.Printf("%s could not delete branch: %v\n%s\n", errorStyle.Render("Warning:"), err, string(deleteBranchOutput))
		fmt.Println("The branch may need to be deleted manually with: git branch -D", branchName)
	}

	// Mark the task as completed
	// Worktree name format: task-{timestamp}-{instance} (e.g., task-1769877109920033000-1)
	// Extract task ID by removing the last -{instance} suffix
	taskID := worktreeName
	if lastDash := strings.LastIndex(worktreeName, "-"); lastDash > 0 {
		taskID = worktreeName[:lastDash]
	}

	tasks, err := loadTasks()
	if err != nil {
		fmt.Printf("%s could not load tasks to update status: %v\n", errorStyle.Render("Warning:"), err)
	} else {
		for i, t := range tasks {
			if t.ID == taskID {
				tasks[i].Status = "completed"
				if err := saveTasks(tasks); err != nil {
					fmt.Printf("%s could not save task status: %v\n", errorStyle.Render("Warning:"), err)
				} else {
					fmt.Printf("Marked task '%s' as completed.\n", taskID)
				}
				break
			}
		}
	}

	fmt.Println()
	fmt.Println(successStyle.Render(fmt.Sprintf("Successfully accepted worktree '%s'", worktreeName)))
	return nil
}

func runDelete(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("task ID required\nRun 'autom8 list' to see task IDs")
	}

	taskID := args[0]

	tasks, err := loadTasks()
	if err != nil {
		return fmt.Errorf("error loading tasks: %w", err)
	}

	// Find the task
	taskIndex := -1
	for i, t := range tasks {
		if t.ID == taskID {
			taskIndex = i
			break
		}
	}

	if taskIndex == -1 {
		return fmt.Errorf("task '%s' not found\nRun 'autom8 list' to see task IDs", taskID)
	}

	// Check if any other tasks depend on this one
	var dependents []string
	for _, t := range tasks {
		if t.DependsOn == taskID {
			dependents = append(dependents, t.ID)
		}
	}

	if len(dependents) > 0 {
		msg := fmt.Sprintf("cannot delete task '%s' because these tasks depend on it:\n", taskID)
		for _, dep := range dependents {
			msg += fmt.Sprintf("  - %s\n", dep)
		}
		msg += "Delete the dependent tasks first, or use a different approach."
		return fmt.Errorf(msg)
	}

	// Remove the task
	tasks = append(tasks[:taskIndex], tasks[taskIndex+1:]...)

	if err := saveTasks(tasks); err != nil {
		return fmt.Errorf("error saving tasks: %w", err)
	}

	fmt.Println(successStyle.Render(fmt.Sprintf("Task '%s' deleted.", taskID)))
	return nil
}

func runImplement(cmd *cobra.Command, args []string) error {
	// Check git repo first
	if _, err := getGitRoot(); err != nil {
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

	tasks, err := loadTasks()
	if err != nil {
		return fmt.Errorf("error loading tasks: %w", err)
	}

	if len(tasks) == 0 {
		fmt.Println(subtitleStyle.Render("No tasks found. Use 'autom8 feature' to create one."))
		return nil
	}

	// Filter tasks to implement
	var pendingTasks []Task
	for _, task := range tasks {
		// If a specific task ID was provided, only include that task
		if targetTaskID != "" {
			if task.ID == targetTaskID {
				if task.Status == "completed" {
					return fmt.Errorf("task '%s' is already completed", targetTaskID)
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
		fmt.Println(subtitleStyle.Render("No pending tasks to implement."))
		return nil
	}

	gitRoot, err := getGitRoot()
	if err != nil {
		return err
	}

	autom8Path, err := ensureAutom8Dir()
	if err != nil {
		return fmt.Errorf("error ensuring autom8 dir: %w", err)
	}

	worktreesDir := filepath.Join(autom8Path, "worktrees")
	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		return fmt.Errorf("error creating worktrees dir: %w", err)
	}

	// Build task map for dependency lookup
	taskMap := make(map[string]Task)
	for _, t := range tasks {
		taskMap[t.ID] = t
	}

	// Separate tasks with and without dependencies
	var independentTasks []Task
	var dependentTasks []Task
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

	fmt.Println(titleStyle.Render("Starting Implementation"))
	fmt.Println()
	fmt.Printf("  %s %d\n", subtitleStyle.Render("Instances per task:"), numInstances)
	fmt.Printf("  %s %d task(s) x %d = %d worktrees\n",
		subtitleStyle.Render("Independent:"), len(independentTasks), numInstances, totalIndependent)
	if len(dependentTasks) > 0 {
		fmt.Printf("  %s %d task(s) x %d^2 = %d worktrees (exponential)\n",
			subtitleStyle.Render("Dependent:"), len(dependentTasks), numInstances, totalDependent)
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
	if err := saveTasks(tasks); err != nil {
		return fmt.Errorf("error updating task status: %w", err)
	}

	var wg sync.WaitGroup
	results := make(chan string, totalIndependent+totalDependent)

	// Track created branches for independent tasks
	independentBranches := make(map[string][]string)

	// Start independent tasks in parallel
	for _, task := range independentTasks {
		independentBranches[task.ID] = make([]string, numInstances)
		for i := 0; i < numInstances; i++ {
			suffix := fmt.Sprintf("-%d", i+1)
			independentBranches[task.ID][i] = suffix
			wg.Add(1)
			go func(t Task, s string) {
				defer wg.Done()
				result := implementTaskWithSuffix(t, gitRoot, worktreesDir, "", s)
				results <- result
			}(task, suffix)
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
				wg.Add(1)
				go func(t Task, ds, s string) {
					defer wg.Done()
					baseBranch := fmt.Sprintf("%s%s", t.DependsOn, ds)
					result := implementTaskWithSuffix(t, gitRoot, worktreesDir, baseBranch, s)
					results <- result
				}(task, depSuffix, suffix)
			}
		}
	}

	// Wait and collect results
	go func() {
		wg.Wait()
		close(results)
	}()

	for result := range results {
		fmt.Println(result)
	}

	fmt.Println()
	fmt.Println(successStyle.Render("All tasks started!"))
	fmt.Println(subtitleStyle.Render("Use 'autom8 status' to check progress."))
	return nil
}

func implementTaskWithSuffix(task Task, gitRoot, worktreesDir, baseBranchID, suffix string) string {
	instanceID := task.ID + suffix
	worktreePath := filepath.Join(worktreesDir, instanceID)

	branchName := fmt.Sprintf("autom8/%s", instanceID)

	// Check if worktree already exists
	if _, err := os.Stat(worktreePath); err == nil {
		return fmt.Sprintf("  %s %s (already exists)", subtitleStyle.Render("[skip]"), instanceID)
	}

	// Determine base branch
	var cmd *exec.Cmd
	if baseBranchID != "" {
		baseBranch := fmt.Sprintf("autom8/%s", baseBranchID)
		cmd = exec.Command("git", "-C", gitRoot, "worktree", "add", "-b", branchName, worktreePath, baseBranch)
	} else {
		cmd = exec.Command("git", "-C", gitRoot, "worktree", "add", "-b", branchName, worktreePath)
	}

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Sprintf("  %s %s: %v\n%s", errorStyle.Render("[error]"), instanceID, err, string(output))
	}

	// Build the prompt with verification criteria
	prompt := task.Prompt
	if len(task.VerificationCriteria) > 0 {
		prompt += "\n\nVerification criteria:\n"
		for _, c := range task.VerificationCriteria {
			prompt += fmt.Sprintf("- %s\n", c)
		}
	}

	// Run claude in the worktree
	claudeCmd := exec.Command("claude", "-p", prompt, "--dangerously-skip-permissions")
	claudeCmd.Dir = worktreePath

	if err := claudeCmd.Start(); err != nil {
		return fmt.Sprintf("  %s %s: failed to start claude: %v", errorStyle.Render("[error]"), instanceID, err)
	}

	// Save the PID for tracking
	savePid(instanceID, claudeCmd.Process.Pid)

	baseInfo := "HEAD"
	if baseBranchID != "" {
		baseInfo = fmt.Sprintf("autom8/%s", baseBranchID)
	}
	return fmt.Sprintf("  %s %s (branch: %s, base: %s)",
		successStyle.Render("[started]"), instanceID, highlightStyle.Render(branchName), idStyle.Render(baseInfo))
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
