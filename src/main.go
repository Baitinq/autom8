package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

const (
	autom8Dir = ".autom8"
	tasksFile = "tasks.json"
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

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all saved tasks",
	Long:    `Display all tasks with their status, prompts, and verification criteria.`,
	RunE:    runList,
}

var implementCmd = &cobra.Command{
	Use:   "implement",
	Short: "Implement all pending tasks using AI",
	Long: `Launch Claude AI agents to implement all pending tasks.

Each agent runs in an isolated git worktree, allowing multiple parallel
implementations without conflicts. For dependent tasks, the branching
is exponential - each instance of a dependent task branches from each
instance of its parent task.`,
	Example: `  # Single implementation per task
  autom8 implement

  # Multiple parallel implementations (useful for exploring solutions)
  autom8 implement -n 3`,
	RunE: runImplement,
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show status of active worktrees and implementations",
	Long:  `Display the status of all active worktrees including branch info, commit status, and whether AI is still working.`,
	RunE:  runStatus,
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
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(implementCmd)
	rootCmd.AddCommand(statusCmd)
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

func runList(cmd *cobra.Command, args []string) error {
	// Check git repo first
	if _, err := getGitRoot(); err != nil {
		return err
	}

	tasks, err := loadTasks()
	if err != nil {
		return fmt.Errorf("error loading tasks: %w", err)
	}

	if len(tasks) == 0 {
		fmt.Println(subtitleStyle.Render("No tasks found. Use 'autom8 feature' to create one."))
		return nil
	}

	fmt.Println(titleStyle.Render(fmt.Sprintf("Tasks (%d)", len(tasks))))
	fmt.Println()

	for i, task := range tasks {
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

		fmt.Printf("%d. %s %s\n", i+1, statusBadge, truncate(task.Prompt, 60))
		fmt.Printf("   %s %s\n", subtitleStyle.Render("ID:"), idStyle.Render(task.ID))
		fmt.Printf("   %s %s\n", subtitleStyle.Render("Created:"), task.CreatedAt.Format("2006-01-02 15:04:05"))

		if task.DependsOn != "" {
			fmt.Printf("   %s %s\n", subtitleStyle.Render("Depends on:"), highlightStyle.Render(task.DependsOn))
		}

		if len(task.VerificationCriteria) > 0 {
			fmt.Printf("   %s\n", subtitleStyle.Render("Verification criteria:"))
			for _, c := range task.VerificationCriteria {
				fmt.Printf("     - %s\n", c)
			}
		}
		fmt.Println()
	}

	return nil
}

func runStatus(cmd *cobra.Command, args []string) error {
	autom8Path, err := getAutom8Dir()
	if err != nil {
		return err
	}

	worktreesDir := filepath.Join(autom8Path, "worktrees")

	// Check if worktrees directory exists
	if _, err := os.Stat(worktreesDir); os.IsNotExist(err) {
		fmt.Println(subtitleStyle.Render("No worktrees found. Use 'autom8 implement' to create implementations."))
		return nil
	}

	// List all worktree directories
	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		return fmt.Errorf("error reading worktrees dir: %w", err)
	}

	if len(entries) == 0 {
		fmt.Println(subtitleStyle.Render("No worktrees found. Use 'autom8 implement' to create implementations."))
		return nil
	}

	fmt.Println(titleStyle.Render(fmt.Sprintf("Worktrees (%d)", len(entries))))
	fmt.Println()

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		worktreeName := entry.Name()
		worktreePath := filepath.Join(worktreesDir, worktreeName)

		// Check if there are any git changes
		statusCmd := exec.Command("git", "-C", worktreePath, "status", "--porcelain")
		statusOutput, err := statusCmd.Output()
		hasChanges := err == nil && len(strings.TrimSpace(string(statusOutput))) > 0

		// Check how many commits are ahead
		aheadCmd := exec.Command("git", "-C", worktreePath, "rev-list", "--count", "HEAD", "^main")
		aheadOutput, err := aheadCmd.Output()
		commitsAhead := "0"
		if err == nil {
			commitsAhead = strings.TrimSpace(string(aheadOutput))
		}

		// Get the branch name
		branchCmd := exec.Command("git", "-C", worktreePath, "branch", "--show-current")
		branchOutput, err := branchCmd.Output()
		branchName := "unknown"
		if err == nil {
			branchName = strings.TrimSpace(string(branchOutput))
		}

		// Check if there are any running processes in the worktree
		processCmd := exec.Command("pgrep", "-f", worktreePath)
		_, err = processCmd.Output()
		isRunning := err == nil

		// Determine status
		var statusText string
		if isRunning {
			statusText = statusInProgressStyle.Render("[running]")
		} else if hasChanges {
			statusText = statusPendingStyle.Render("[modified]")
		} else if commitsAhead != "0" {
			statusText = statusCompletedStyle.Render("[committed]")
		} else {
			statusText = subtitleStyle.Render("[idle]")
		}

		fmt.Printf("%s %s\n", statusText, worktreeName)
		fmt.Printf("   %s %s\n", subtitleStyle.Render("Branch:"), highlightStyle.Render(branchName))
		fmt.Printf("   %s %s\n", subtitleStyle.Render("Commits ahead:"), commitsAhead)
		if hasChanges {
			fmt.Printf("   %s %s\n", subtitleStyle.Render("Uncommitted:"), "yes")
		}
		fmt.Printf("   %s %s\n", subtitleStyle.Render("Path:"), idStyle.Render(worktreePath))
		fmt.Println()
	}

	fmt.Println(subtitleStyle.Render("Tip: cd into a worktree to see detailed changes with 'git status' and 'git log'"))
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

	tasks, err := loadTasks()
	if err != nil {
		return fmt.Errorf("error loading tasks: %w", err)
	}

	if len(tasks) == 0 {
		fmt.Println(subtitleStyle.Render("No tasks found. Use 'autom8 feature' to create one."))
		return nil
	}

	// Filter pending tasks
	var pendingTasks []Task
	for _, task := range tasks {
		if task.Status == "pending" {
			pendingTasks = append(pendingTasks, task)
		}
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
