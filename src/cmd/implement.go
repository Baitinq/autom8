package cmd

import (
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/baitinq/autom8/src/core"
	"github.com/spf13/cobra"
)

//go:embed agents/*.md
var agentTemplates embed.FS

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
instance of its parent task.`,
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

	// Load the implementer agent template
	agentTemplate, err := loadAgentTemplate("implementer")
	if err != nil {
		// Template is optional, continue without it
		agentTemplate = ""
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
			go func(t core.Task, s string) {
				defer wg.Done()
				result := implementTaskWithSuffix(t, gitRoot, worktreesDir, "", s, agentTemplate, maxIterations)
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
				go func(t core.Task, ds, s string) {
					defer wg.Done()
					baseBranch := fmt.Sprintf("%s%s", t.DependsOn, ds)
					result := implementTaskWithSuffix(t, gitRoot, worktreesDir, baseBranch, s, agentTemplate, maxIterations)
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
	fmt.Println(SuccessStyle.Render("All implementations complete!"))
	fmt.Println(SubtitleStyle.Render("Use 'autom8 status' to see results."))
	return nil
}

func implementTaskWithSuffix(task core.Task, gitRoot, worktreesDir, baseBranchID, suffix, agentTemplate string, maxIter int) string {
	instanceID := task.ID + suffix
	worktreePath := filepath.Join(worktreesDir, instanceID)

	branchName := fmt.Sprintf("autom8/%s", instanceID)

	// Check if worktree already exists
	if _, err := os.Stat(worktreePath); err == nil {
		return fmt.Sprintf("  %s %s (already exists)", SubtitleStyle.Render("[skip]"), instanceID)
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

	// Run claude in a loop until TASK COMPLETE or max iterations
	iteration := 0
	for {
		iteration++

		// Check max iterations limit
		if maxIter > 0 && iteration > maxIter {
			return fmt.Sprintf("  %s %s (max iterations %d reached)", StatusPendingStyle.Render("[stopped]"), instanceID, maxIter)
		}

		// Create log file for this iteration
		logFile := filepath.Join(logsDir, fmt.Sprintf("iteration-%d.log", iteration))

		// Run claude synchronously and capture output
		claudeCmd := exec.Command("claude", "-p", prompt, "--dangerously-skip-permissions")
		claudeCmd.Dir = worktreePath

		output, err := claudeCmd.Output()
		if err != nil {
			// Log the error
			os.WriteFile(logFile, []byte(fmt.Sprintf("ERROR: %v\n%s", err, string(output))), 0644)
			return fmt.Sprintf("  %s %s (iteration %d failed: %v)", ErrorStyle.Render("[error]"), instanceID, iteration, err)
		}

		// Write output to log file
		os.WriteFile(logFile, output, 0644)

		// Check if output contains TASK COMPLETE
		if strings.Contains(string(output), "TASK COMPLETE") {
			// Implementation complete - now start the review loop
			reviewResult := runReviewLoop(task, worktreePath, logsDir, baseBranch)
			if reviewResult != "" {
				return fmt.Sprintf("  %s %s (review failed: %s)", ErrorStyle.Render("[error]"), instanceID, reviewResult)
			}

			baseInfo := "HEAD"
			if baseBranchID != "" {
				baseInfo = fmt.Sprintf("autom8/%s", baseBranchID)
			}
			return fmt.Sprintf("  %s %s (branch: %s, base: %s, impl iterations: %d)",
				SuccessStyle.Render("[completed]"), instanceID, HighlightStyle.Render(branchName), IDStyle.Render(baseInfo), iteration)
		}

		// Continue to next iteration
	}
}

// runReviewLoop runs the review loop after implementation completes.
// It uses codex review to check the implementation and codex exec to fix issues.
// Returns empty string on success, or an error message on failure.
func runReviewLoop(task core.Task, worktreePath, logsDir, baseBranch string) string {
	// Load the reviewer agent template
	reviewerTemplate, err := loadAgentTemplate("reviewer")
	if err != nil {
		reviewerTemplate = ""
	}

	reviewIteration := 0
	fixIteration := 0

	for {
		reviewIteration++

		// Build the review prompt
		reviewPrompt := buildReviewPrompt(task, reviewerTemplate)

		// Create log file for this review iteration
		reviewLogFile := filepath.Join(logsDir, fmt.Sprintf("review-iteration-%d.log", reviewIteration))

		// Run codex review with base branch (prompt via stdin using "-")
		codexCmd := exec.Command("codex", "review", "--base", baseBranch, "-")
		codexCmd.Dir = worktreePath
		codexCmd.Stdin = strings.NewReader(reviewPrompt)

		output, err := codexCmd.CombinedOutput()
		if err != nil {
			// Log the error with full output
			os.WriteFile(reviewLogFile, []byte(fmt.Sprintf("ERROR: %v\n\nOutput:\n%s", err, string(output))), 0644)
			return fmt.Sprintf("review iteration %d failed: %v", reviewIteration, err)
		}

		// Write output to log file
		os.WriteFile(reviewLogFile, output, 0644)

		// Check if review is approved
		if strings.Contains(string(output), "REVIEW APPROVED") {
			return "" // Success - review approved
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
			return fmt.Sprintf("fix iteration %d failed: %v", fixIteration, err)
		}

		// Write output to log file
		os.WriteFile(fixLogFile, fixOutput, 0644)

		// Continue to next review iteration
	}
}

// buildReviewPrompt constructs the prompt for the codex review command.
func buildReviewPrompt(task core.Task, reviewerTemplate string) string {
	var sb strings.Builder

	if reviewerTemplate != "" {
		sb.WriteString(reviewerTemplate)
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

	sb.WriteString("Review the implementation changes and determine if they satisfy all requirements and verification criteria.\n")
	sb.WriteString("If satisfied, output: REVIEW APPROVED\n")
	sb.WriteString("If issues found, provide specific feedback for the implementer.\n")

	return sb.String()
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
