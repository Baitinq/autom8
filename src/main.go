package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	autom8Dir = ".autom8"
	tasksFile = "tasks.json"
)

type Task struct {
	ID                   string    `json:"id"`
	Prompt               string    `json:"prompt"`
	VerificationCriteria []string  `json:"verification_criteria"`
	DependsOn            string    `json:"depends_on,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
	Status               string    `json:"status"`
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	// Help doesn't require git repo
	if command == "help" || command == "-h" || command == "--help" {
		printUsage()
		return
	}

	// All other commands require git repo
	if _, err := getGitRoot(); err != nil {
		fmt.Println("Error: must be run inside a git repository")
		os.Exit(1)
	}

	switch command {
	case "feature":
		runFeature()
	case "implement":
		runImplement()
	case "list":
		runList()
	case "status":
		runStatus()
	case "accept":
		runAccept()
	case "delete":
		runDelete()
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func getGitRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func printUsage() {
	fmt.Println("autom8 - Automate AI agent workflows")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  autom8 <command> [options]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  feature    Create a new task/prompt")
	fmt.Println("             -p <prompt>    Task prompt (non-interactive)")
	fmt.Println("             -c <criteria>  Verification criteria (repeatable)")
	fmt.Println("             -d <task-id>   Depends on another task")
	fmt.Println("  list       List all saved tasks")
	fmt.Println("  implement  Implement all pending tasks using AI")
	fmt.Println("             -n <num>       Number of parallel instances per task (default: 1)")
	fmt.Println("             Creates git worktrees in .autom8/worktrees/")
	fmt.Println("             Dependent tasks branch from their dependency (exponential)")
	fmt.Println("  status     Show status of active worktrees and implementations")
	fmt.Println("  accept     Merge a worktree branch into current branch and clean up")
	fmt.Println("             <worktree>     Name of the worktree to accept")
	fmt.Println("  delete     Delete a task by ID")
	fmt.Println("             <task-id>      ID of the task to delete")
	fmt.Println("  help       Show this help message")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  autom8 feature -p \"Add login page\" -c \"Has email field\"")
	fmt.Println("  autom8 feature -p \"Add logout\" -d task-123  # depends on task-123")
	fmt.Println("  autom8 list")
	fmt.Println("  autom8 implement")
	fmt.Println("  autom8 accept task-123456-1")
	fmt.Println("  autom8 delete task-123456789")
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

func readMultilineInput(reader *bufio.Reader) string {
	var lines []string

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}

		line = strings.TrimRight(line, "\r\n")

		if line == "" {
			break
		}
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

type arrayFlags []string

func (a *arrayFlags) String() string {
	return strings.Join(*a, ", ")
}

func (a *arrayFlags) Set(value string) error {
	*a = append(*a, value)
	return nil
}

func runFeature() {
	featureCmd := flag.NewFlagSet("feature", flag.ExitOnError)
	promptFlag := featureCmd.String("p", "", "Task prompt (non-interactive mode)")
	dependsOnFlag := featureCmd.String("d", "", "Task ID this depends on")
	var criteriaFlags arrayFlags
	featureCmd.Var(&criteriaFlags, "c", "Verification criteria (can be specified multiple times)")

	featureCmd.Parse(os.Args[2:])

	var prompt string
	var criteria []string
	var dependsOn string

	if *promptFlag != "" {
		// Non-interactive mode
		prompt = *promptFlag
		criteria = criteriaFlags
		dependsOn = *dependsOnFlag
	} else {
		// Interactive mode
		reader := bufio.NewReader(os.Stdin)

		fmt.Println("Enter your task/prompt (press Enter to finish):")
		fmt.Println()

		prompt = readMultilineInput(reader)

		if strings.TrimSpace(prompt) == "" {
			fmt.Println("No prompt entered. Aborting.")
			return
		}

		fmt.Println()
		fmt.Println("Enter verification criteria (one per line, empty line to finish):")
		fmt.Println()

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				break
			}

			line = strings.TrimRight(line, "\r\n")

			if line == "" {
				break
			}
			criteria = append(criteria, line)
		}

		fmt.Println()
		fmt.Print("Depends on task ID (leave empty if none): ")
		depLine, _ := reader.ReadString('\n')
		dependsOn = strings.TrimSpace(depLine)
	}

	if strings.TrimSpace(prompt) == "" {
		fmt.Println("No prompt provided. Aborting.")
		return
	}

	tasks, err := loadTasks()
	if err != nil {
		fmt.Printf("Error loading tasks: %v\n", err)
		os.Exit(1)
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
			fmt.Printf("Error: dependency task '%s' not found\n", dependsOn)
			os.Exit(1)
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
		fmt.Printf("Error saving task: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Printf("Task saved with ID: %s\n", task.ID)
}

func runList() {
	tasks, err := loadTasks()
	if err != nil {
		fmt.Printf("Error loading tasks: %v\n", err)
		os.Exit(1)
	}

	if len(tasks) == 0 {
		fmt.Println("No tasks found. Use 'autom8 feature' to create one.")
		return
	}

	fmt.Printf("Found %d task(s):\n\n", len(tasks))

	for i, task := range tasks {
		fmt.Printf("%d. [%s] %s\n", i+1, task.Status, truncate(task.Prompt, 60))
		fmt.Printf("   ID: %s\n", task.ID)
		fmt.Printf("   Created: %s\n", task.CreatedAt.Format("2006-01-02 15:04:05"))
		if task.DependsOn != "" {
			fmt.Printf("   Depends on: %s\n", task.DependsOn)
		}
		if len(task.VerificationCriteria) > 0 {
			fmt.Println("   Verification criteria:")
			for _, c := range task.VerificationCriteria {
				fmt.Printf("     - %s\n", c)
			}
		}
		fmt.Println()
	}
}

func runStatus() {
	autom8Path, err := getAutom8Dir()
	if err != nil {
		fmt.Printf("Error getting autom8 dir: %v\n", err)
		os.Exit(1)
	}

	worktreesDir := filepath.Join(autom8Path, "worktrees")

	// Check if worktrees directory exists
	if _, err := os.Stat(worktreesDir); os.IsNotExist(err) {
		fmt.Println("No worktrees found. Use 'autom8 implement' to create implementations.")
		return
	}

	// List all worktree directories
	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		fmt.Printf("Error reading worktrees dir: %v\n", err)
		os.Exit(1)
	}

	if len(entries) == 0 {
		fmt.Println("No worktrees found. Use 'autom8 implement' to create implementations.")
		return
	}

	fmt.Printf("Found %d worktree(s):\n\n", len(entries))

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
		// This is a simple check - look for claude processes
		processCmd := exec.Command("pgrep", "-f", worktreePath)
		_, err = processCmd.Output()
		isRunning := err == nil

		status := "idle"
		if isRunning {
			status = "running"
		} else if hasChanges {
			status = "modified"
		} else if commitsAhead != "0" {
			status = "committed"
		}

		fmt.Printf("📁 %s\n", worktreeName)
		fmt.Printf("   Status: %s", status)
		if isRunning {
			fmt.Printf(" (AI working)")
		}
		fmt.Println()
		fmt.Printf("   Branch: %s\n", branchName)
		fmt.Printf("   Commits ahead: %s\n", commitsAhead)
		if hasChanges {
			fmt.Printf("   Uncommitted changes: yes\n")
		}
		fmt.Printf("   Path: %s\n", worktreePath)

		// Show accept hint if there's something to accept (commits or uncommitted changes)
		if !isRunning && (commitsAhead != "0" || hasChanges) {
			fmt.Printf("   ➡️  To accept: autom8 accept %s\n", worktreeName)
		}
		fmt.Println()
	}

	fmt.Println("💡 Tip: cd into a worktree to see detailed changes with 'git status' and 'git log'")
}

func runAccept() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: autom8 accept <worktree-name>")
		fmt.Println("Run 'autom8 status' to see available worktrees.")
		os.Exit(1)
	}

	worktreeName := os.Args[2]

	gitRoot, err := getGitRoot()
	if err != nil {
		fmt.Printf("Error getting git root: %v\n", err)
		os.Exit(1)
	}

	autom8Path, err := getAutom8Dir()
	if err != nil {
		fmt.Printf("Error getting autom8 dir: %v\n", err)
		os.Exit(1)
	}

	worktreePath := filepath.Join(autom8Path, "worktrees", worktreeName)

	// Check if worktree exists
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		fmt.Printf("Error: worktree '%s' not found\n", worktreeName)
		fmt.Println("Run 'autom8 status' to see available worktrees.")
		os.Exit(1)
	}

	// Get the branch name from the worktree
	branchCmd := exec.Command("git", "-C", worktreePath, "branch", "--show-current")
	branchOutput, err := branchCmd.Output()
	if err != nil {
		fmt.Printf("Error getting branch name: %v\n", err)
		os.Exit(1)
	}
	branchName := strings.TrimSpace(string(branchOutput))

	if branchName == "" {
		fmt.Println("Error: could not determine branch name for worktree")
		os.Exit(1)
	}

	// Check for uncommitted changes in the worktree
	statusCmd := exec.Command("git", "-C", worktreePath, "status", "--porcelain")
	statusOutput, err := statusCmd.Output()
	if err != nil {
		fmt.Printf("Error checking worktree status: %v\n", err)
		os.Exit(1)
	}

	if len(strings.TrimSpace(string(statusOutput))) > 0 {
		fmt.Println("Found uncommitted changes, auto-committing...")

		// Stage all changes
		addCmd := exec.Command("git", "-C", worktreePath, "add", "-A")
		if addOutput, err := addCmd.CombinedOutput(); err != nil {
			fmt.Printf("Error staging changes: %v\n%s\n", err, string(addOutput))
			os.Exit(1)
		}

		// Commit with auto-commit message
		commitCmd := exec.Command("git", "-C", worktreePath, "commit", "-m", "autom8: auto-commit uncommitted changes")
		if commitOutput, err := commitCmd.CombinedOutput(); err != nil {
			fmt.Printf("Error committing changes: %v\n%s\n", err, string(commitOutput))
			os.Exit(1)
		}
		fmt.Println("Auto-committed successfully.")
	}

	fmt.Printf("Merging branch '%s' into current branch...\n", branchName)

	// Merge the branch into the current branch
	mergeCmd := exec.Command("git", "-C", gitRoot, "merge", branchName, "-m", fmt.Sprintf("Merge %s (autom8 accept)", branchName))
	mergeOutput, err := mergeCmd.CombinedOutput()
	if err != nil {
		fmt.Printf("Error merging branch: %v\n%s\n", err, string(mergeOutput))
		fmt.Println("Resolve conflicts manually, then run 'autom8 accept' again to clean up.")
		os.Exit(1)
	}
	fmt.Printf("%s", string(mergeOutput))

	// Remove the worktree
	fmt.Printf("Removing worktree '%s'...\n", worktreeName)
	removeCmd := exec.Command("git", "-C", gitRoot, "worktree", "remove", worktreePath)
	removeOutput, err := removeCmd.CombinedOutput()
	if err != nil {
		fmt.Printf("Error removing worktree: %v\n%s\n", err, string(removeOutput))
		fmt.Println("You may need to manually remove it with: git worktree remove", worktreePath)
		os.Exit(1)
	}

	// Delete the branch (it's been merged)
	fmt.Printf("Deleting branch '%s'...\n", branchName)
	deleteBranchCmd := exec.Command("git", "-C", gitRoot, "branch", "-d", branchName)
	deleteBranchOutput, err := deleteBranchCmd.CombinedOutput()
	if err != nil {
		fmt.Printf("Warning: could not delete branch: %v\n%s\n", err, string(deleteBranchOutput))
		fmt.Println("The branch may need to be deleted manually with: git branch -D", branchName)
	}

	fmt.Printf("\nSuccessfully accepted worktree '%s'\n", worktreeName)
}

func runDelete() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: autom8 delete <task-id>")
		fmt.Println("Run 'autom8 list' to see task IDs.")
		os.Exit(1)
	}

	taskID := os.Args[2]

	tasks, err := loadTasks()
	if err != nil {
		fmt.Printf("Error loading tasks: %v\n", err)
		os.Exit(1)
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
		fmt.Printf("Error: task '%s' not found\n", taskID)
		fmt.Println("Run 'autom8 list' to see task IDs.")
		os.Exit(1)
	}

	// Check if any other tasks depend on this one
	var dependents []string
	for _, t := range tasks {
		if t.DependsOn == taskID {
			dependents = append(dependents, t.ID)
		}
	}

	if len(dependents) > 0 {
		fmt.Printf("Error: cannot delete task '%s' because these tasks depend on it:\n", taskID)
		for _, dep := range dependents {
			fmt.Printf("  - %s\n", dep)
		}
		fmt.Println("Delete the dependent tasks first, or use a different approach.")
		os.Exit(1)
	}

	// Remove the task
	tasks = append(tasks[:taskIndex], tasks[taskIndex+1:]...)

	if err := saveTasks(tasks); err != nil {
		fmt.Printf("Error saving tasks: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Task '%s' deleted.\n", taskID)
}

func runImplement() {
	implementCmd := flag.NewFlagSet("implement", flag.ExitOnError)
	numInstances := implementCmd.Int("n", 1, "Number of parallel instances per task")
	implementCmd.Parse(os.Args[2:])

	if *numInstances < 1 {
		*numInstances = 1
	}

	tasks, err := loadTasks()
	if err != nil {
		fmt.Printf("Error loading tasks: %v\n", err)
		os.Exit(1)
	}

	if len(tasks) == 0 {
		fmt.Println("No tasks found. Use 'autom8 feature' to create one.")
		return
	}

	// Filter pending tasks
	var pendingTasks []Task
	for _, task := range tasks {
		if task.Status == "pending" {
			pendingTasks = append(pendingTasks, task)
		}
	}

	if len(pendingTasks) == 0 {
		fmt.Println("No pending tasks to implement.")
		return
	}

	gitRoot, err := getGitRoot()
	if err != nil {
		fmt.Printf("Error getting git root: %v\n", err)
		os.Exit(1)
	}

	autom8Path, err := ensureAutom8Dir()
	if err != nil {
		fmt.Printf("Error ensuring autom8 dir: %v\n", err)
		os.Exit(1)
	}

	worktreesDir := filepath.Join(autom8Path, "worktrees")
	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		fmt.Printf("Error creating worktrees dir: %v\n", err)
		os.Exit(1)
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
	totalIndependent := len(independentTasks) * *numInstances
	totalDependent := len(dependentTasks) * *numInstances * *numInstances // Each dependent branches from each instance

	fmt.Printf("Implementing with %d instance(s) per task...\n", *numInstances)
	fmt.Printf("  Independent: %d task(s) x %d = %d worktrees\n", len(independentTasks), *numInstances, totalIndependent)
	if len(dependentTasks) > 0 {
		fmt.Printf("  Dependent: %d task(s) x %d^2 = %d worktrees (exponential)\n", len(dependentTasks), *numInstances, totalDependent)
	}
	fmt.Println()

	var wg sync.WaitGroup
	results := make(chan string, totalIndependent+totalDependent)

	// Track created branches for independent tasks (so dependents can branch from them)
	independentBranches := make(map[string][]string) // taskID -> list of branch suffixes

	// Start independent tasks in parallel (n instances each)
	for _, task := range independentTasks {
		independentBranches[task.ID] = make([]string, *numInstances)
		for i := 0; i < *numInstances; i++ {
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

	// Start dependent tasks (branch from each instance of dependency)
	for _, task := range dependentTasks {
		depSuffixes := independentBranches[task.DependsOn]
		if depSuffixes == nil {
			// Dependency might also be dependent, just use default suffixes
			depSuffixes = make([]string, *numInstances)
			for i := 0; i < *numInstances; i++ {
				depSuffixes[i] = fmt.Sprintf("-%d", i+1)
			}
		}

		for _, depSuffix := range depSuffixes {
			for i := 0; i < *numInstances; i++ {
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

	// Wait for all tasks to complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Print results as they come in
	for result := range results {
		fmt.Println(result)
	}

	fmt.Println("\nAll tasks started. Check worktrees for progress.")
}

func implementTaskWithSuffix(task Task, gitRoot, worktreesDir, baseBranchID, suffix string) string {
	instanceID := task.ID + suffix
	worktreePath := filepath.Join(worktreesDir, instanceID)

	// Create branch name from task ID + suffix
	branchName := fmt.Sprintf("autom8/%s", instanceID)

	// Check if worktree already exists
	if _, err := os.Stat(worktreePath); err == nil {
		return fmt.Sprintf("[%s] Worktree already exists at %s", instanceID, worktreePath)
	}

	// Determine base branch
	var cmd *exec.Cmd
	if baseBranchID != "" {
		// Branch from dependency's branch
		baseBranch := fmt.Sprintf("autom8/%s", baseBranchID)
		cmd = exec.Command("git", "-C", gitRoot, "worktree", "add", "-b", branchName, worktreePath, baseBranch)
	} else {
		// Branch from current HEAD
		cmd = exec.Command("git", "-C", gitRoot, "worktree", "add", "-b", branchName, worktreePath)
	}

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Sprintf("[%s] Error creating worktree: %v\n%s", instanceID, err, string(output))
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

	// Start claude in background (don't wait for it)
	if err := claudeCmd.Start(); err != nil {
		return fmt.Sprintf("[%s] Error starting claude: %v", instanceID, err)
	}

	baseInfo := "HEAD"
	if baseBranchID != "" {
		baseInfo = fmt.Sprintf("autom8/%s", baseBranchID)
	}
	return fmt.Sprintf("[%s] Started implementation in %s (branch: %s, base: %s)", instanceID, worktreePath, branchName, baseInfo)
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
