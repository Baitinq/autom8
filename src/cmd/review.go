package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/baitinq/autom8/src/core"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

var (
	reviewNumInstances int
	reviewNameFlag     string
)

var ReviewCmd = &cobra.Command{
	Use:   "review [PR#|branch]",
	Short: "Create a code review task for a PR or branch",
	Long: `Create a review task that spawns N parallel code reviewers.

If no argument is provided, reviews the current branch against main.
You can specify a PR number or branch name to review.

The review task is created and returns immediately. Use 'autom8 implement'
to run the reviewers, or they will run automatically if you have pending
implementation tasks.

Review results are saved to .autom8/reviews/<task-id>.json.`,
	Example: `  # Review current branch vs main
  autom8 review

  # Review a specific PR
  autom8 review 123

  # Review a specific branch
  autom8 review feature-branch

  # Run 3 parallel reviewers
  autom8 review -n 3

  # Specify a task name
  autom8 review 123 --name pr-123-review`,
	Args: cobra.MaximumNArgs(1),
	RunE: runReview,
}

func init() {
	ReviewCmd.Flags().IntVarP(&reviewNumInstances, "instances", "n", 1, "Number of parallel reviewers")
	ReviewCmd.Flags().StringVar(&reviewNameFlag, "name", "", "Task name (auto-generated if not provided)")
}

// ReviewIssue represents a single code review issue.
type ReviewIssue struct {
	Severity    string `json:"severity"`
	File        string `json:"file"`
	Line        int    `json:"line,omitempty"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// ReviewResult represents the output from a single reviewer.
type ReviewResult struct {
	Issues  []ReviewIssue `json:"issues"`
	Summary string        `json:"summary"`
}

// ReviewTaskResult represents the final result of a review task.
type ReviewTaskResult struct {
	TaskID       string       `json:"task_id"`
	ReviewTarget string       `json:"review_target"`
	CompletedAt  time.Time    `json:"completed_at"`
	Result       ReviewResult `json:"result"`
}

func runReview(cmd *cobra.Command, args []string) error {
	// Check git repo first
	gitRoot, err := core.GetGitRoot()
	if err != nil {
		return err
	}

	// Determine what to review (PR#, branch, or current branch)
	var reviewTarget string
	if len(args) > 0 {
		reviewTarget = args[0]
	} else {
		// Default to current branch
		branchCmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
		branchCmd.Dir = gitRoot
		output, err := branchCmd.Output()
		if err != nil {
			return fmt.Errorf("failed to get current branch: %w", err)
		}
		reviewTarget = strings.TrimSpace(string(output))
	}

	// Load existing tasks
	existingTasks, err := core.LoadTasks()
	if err != nil {
		return fmt.Errorf("error loading tasks: %w", err)
	}

	// Determine task name
	var taskName string
	if reviewNameFlag != "" {
		taskName = reviewNameFlag
		if err := core.ValidateTaskName(taskName); err != nil {
			return err
		}
		if !core.IsTaskNameUnique(existingTasks, taskName, "") {
			return fmt.Errorf("task name '%s' already exists", taskName)
		}
	} else {
		// Interactive mode - ask for name or auto-generate
		var useAutoName bool
		autoName := generateReviewTaskName(reviewTarget)

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Task Name").
					Description(fmt.Sprintf("Use auto-generated name '%s'?", autoName)).
					Value(&useAutoName),
			),
		).WithTheme(huh.ThemeDracula())

		if err := form.Run(); err != nil {
			if err == huh.ErrUserAborted {
				fmt.Println("\nAborted.")
				return nil
			}
			return err
		}

		if useAutoName {
			taskName = autoName
		} else {
			// Ask for custom name
			form = huh.NewForm(
				huh.NewGroup(
					huh.NewInput().
						Title("Custom Task Name").
						Description("Enter a unique identifier for this review task").
						Placeholder("my-review").
						Value(&taskName).
						Validate(func(s string) error {
							if err := core.ValidateTaskName(s); err != nil {
								return err
							}
							if !core.IsTaskNameUnique(existingTasks, s, "") {
								return fmt.Errorf("task name '%s' already exists", s)
							}
							return nil
						}),
				),
			).WithTheme(huh.ThemeDracula())

			if err := form.Run(); err != nil {
				if err == huh.ErrUserAborted {
					fmt.Println("\nAborted.")
					return nil
				}
				return err
			}
		}
	}

	// Ensure the name is unique (might have changed since form)
	if !core.IsTaskNameUnique(existingTasks, taskName, "") {
		return fmt.Errorf("task name '%s' already exists", taskName)
	}

	// Normalize reviewer count to at least 1
	reviewerCount := reviewNumInstances
	if reviewerCount < 1 {
		reviewerCount = 1
	}

	// Create the review task
	task := core.Task{
		ID:            taskName,
		Type:          core.TaskTypeReview,
		Prompt:        fmt.Sprintf("Review %s", reviewTarget),
		ReviewTarget:  reviewTarget,
		ReviewerCount: reviewerCount,
		CreatedAt:     time.Now(),
		Status:        core.TaskStatusPending,
	}

	tasks := append(existingTasks, task)

	if err := core.SaveTasks(tasks); err != nil {
		return fmt.Errorf("error saving task: %w", err)
	}

	fmt.Println()
	fmt.Println(SuccessStyle.Render("Review task created!"))
	fmt.Printf("  %s %s\n", SubtitleStyle.Render("ID:"), IDStyle.Render(task.ID))
	fmt.Printf("  %s %s\n", SubtitleStyle.Render("Type:"), HighlightStyle.Render("review"))
	fmt.Printf("  %s %s\n", SubtitleStyle.Render("Target:"), HighlightStyle.Render(reviewTarget))
	fmt.Printf("  %s %d\n", SubtitleStyle.Render("Reviewers:"), reviewerCount)
	fmt.Println()
	fmt.Println(SubtitleStyle.Render("Run 'autom8 implement' to start the review."))
	return nil
}

// generateReviewTaskName creates an auto-generated name for a review task.
// It sanitizes the target to ensure the resulting name passes ValidateTaskName.
func generateReviewTaskName(target string) string {
	// Build sanitized string character by character
	var sanitized strings.Builder
	for _, r := range target {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			sanitized.WriteRune(r)
		} else if r == '-' || r == '_' {
			sanitized.WriteRune(r)
		} else {
			// Replace any other character (/, ., @, #, space, etc.) with dash
			sanitized.WriteRune('-')
		}
	}

	result := sanitized.String()

	// Collapse multiple consecutive dashes into one
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}

	// Trim leading/trailing dashes
	result = strings.Trim(result, "-")

	// Limit length (leaving room for "review-" prefix, max total is 50)
	maxLen := core.MaxTaskNameLength - len("review-")
	if len(result) > maxLen {
		result = result[:maxLen]
		// Trim trailing dash that may have been created by truncation
		result = strings.TrimRight(result, "-")
	}

	// If empty after sanitization, use a fallback
	if result == "" {
		result = "unnamed"
	}

	// Prefix with "review-"
	return fmt.Sprintf("review-%s", result)
}

// ExecuteReviewTask runs the actual review process for a review task.
// This is called from the worker when processing a review-type task.
// It returns the consolidated result and saves it to .autom8/reviews/<task-id>.json.
func ExecuteReviewTask(task core.Task, gitRoot string, logFunc func(string, ...interface{})) (*ReviewResult, error) {
	reviewTarget := task.ReviewTarget
	numReviewers := task.ReviewerCount
	if numReviewers < 1 {
		numReviewers = 1
	}

	// Load configuration for reviewer tool/model
	cfg, err := core.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	reviewerCfg := cfg.Reviewer

	logFunc("Starting code review")
	logFunc("  Target: %s", reviewTarget)
	logFunc("  Reviewers: %d", numReviewers)
	logFunc("  Tool: %s", reviewerCfg.Tool)

	// Load the code-reviewer agent template
	codeReviewerTemplate, err := loadAgentTemplate("code-reviewer")
	if err != nil {
		codeReviewerTemplate = ""
	}

	// Spawn N parallel reviewers
	var wg sync.WaitGroup
	results := make([]ReviewResult, numReviewers)
	errors := make([]error, numReviewers)

	for i := 0; i < numReviewers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			// Build reviewer prompt
			prompt := buildCodeReviewPrompt(codeReviewerTemplate, reviewTarget)

			// Run the reviewer (no output file needed, we capture in memory)
			result, err := runCodeReviewerInMemory(reviewerCfg, prompt, gitRoot)
			if err != nil {
				errors[idx] = err
				return
			}
			results[idx] = result

			logFunc("Reviewer %d completed (%d issues found)", idx+1, len(result.Issues))
		}(i)
	}

	wg.Wait()

	// Check for errors
	var successfulResults []ReviewResult
	for i := 0; i < numReviewers; i++ {
		if errors[i] != nil {
			logFunc("Reviewer %d failed: %v", i+1, errors[i])
		} else {
			successfulResults = append(successfulResults, results[i])
		}
	}

	if len(successfulResults) == 0 {
		return nil, fmt.Errorf("all reviewers failed")
	}

	logFunc("Consolidating %d successful reviews...", len(successfulResults))

	// Converge the results into a single deduplicated summary
	consolidated := convergeReviews(successfulResults, reviewerCfg, gitRoot)

	// Save the result to .autom8/reviews/<task-id>.json
	reviewsDir, err := core.EnsureReviewsDir()
	if err != nil {
		return &consolidated, fmt.Errorf("failed to ensure reviews dir: %w", err)
	}

	resultFile := filepath.Join(reviewsDir, task.ID+".json")
	taskResult := ReviewTaskResult{
		TaskID:       task.ID,
		ReviewTarget: reviewTarget,
		CompletedAt:  time.Now(),
		Result:       consolidated,
	}

	resultJSON, err := json.MarshalIndent(taskResult, "", "  ")
	if err != nil {
		return &consolidated, fmt.Errorf("failed to marshal result: %w", err)
	}

	if err := os.WriteFile(resultFile, resultJSON, 0644); err != nil {
		return &consolidated, fmt.Errorf("failed to write result file: %w", err)
	}

	logFunc("Review results saved to %s", resultFile)

	return &consolidated, nil
}

// runCodeReviewerInMemory runs a code reviewer and returns the result directly.
func runCodeReviewerInMemory(cfg core.ToolConfig, prompt, workDir string) (ReviewResult, error) {
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
			"-p", "-",
			"--output-format", "json",
			"--dangerously-skip-permissions",
		}
		if cfg.Model != "" {
			args = append(args, "--model", cfg.Model)
		}
		cmd = exec.Command("claude", args...)
		cmd.Stdin = strings.NewReader(prompt)
	}

	cmd.Dir = workDir

	output, err := cmd.Output()
	if err != nil {
		return ReviewResult{}, fmt.Errorf("reviewer command failed: %w", err)
	}

	// Parse the review output
	return parseReviewOutput(string(output))
}

func buildCodeReviewPrompt(template, target string) string {
	var sb strings.Builder

	if template != "" {
		sb.WriteString(template)
	}

	sb.WriteString(fmt.Sprintf("Review target: %s\n\n", target))
	sb.WriteString("Use git diff and/or gh commands to get the diff for the target.\n")
	sb.WriteString("- If the target looks like a PR number, use: gh pr diff <number>\n")
	sb.WriteString("- If it's a branch name, use: git diff main...<branch>\n")
	sb.WriteString("- If it's the current branch, use: git diff main...HEAD\n\n")

	return sb.String()
}

func parseReviewOutput(output string) (ReviewResult, error) {
	// Try to parse JSON response from Claude's --output-format json
	var jsonResp struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal([]byte(output), &jsonResp); err == nil && jsonResp.Result != "" {
		output = jsonResp.Result
	}

	// Find any JSON object in the output by locating first '{' and last '}'
	var result ReviewResult

	start := strings.Index(output, "{")
	end := strings.LastIndex(output, "}")

	if start != -1 && end != -1 && end > start {
		jsonStr := output[start : end+1]
		if err := json.Unmarshal([]byte(jsonStr), &result); err == nil {
			return result, nil
		}
	}

	// Fallback: treat the whole output as a summary
	result.Summary = strings.TrimSpace(output)
	return result, nil
}

func convergeReviews(results []ReviewResult, cfg core.ToolConfig, gitRoot string) ReviewResult {
	// If only one result, still deduplicate and sort it
	if len(results) == 1 {
		return deduplicateAndSort(results[0])
	}

	// Build a prompt to converge the reviews
	prompt := buildReviewConvergePrompt(results)

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
	default: // "claude"
		args := []string{
			"-p", "-",
			"--output-format", "json",
		}
		if cfg.Model != "" {
			args = append(args, "--model", cfg.Model)
		}
		cmd = exec.Command("claude", args...)
		cmd.Stdin = strings.NewReader(prompt)
	}

	cmd.Dir = gitRoot

	output, err := cmd.Output()
	if err != nil {
		// If convergence fails, merge manually
		return mergeReviewsManually(results)
	}

	result, err := parseReviewOutput(string(output))
	if err != nil || len(result.Issues) == 0 && result.Summary == "" {
		return mergeReviewsManually(results)
	}

	// Always deduplicate and sort, even after AI convergence
	return deduplicateAndSort(result)
}

func buildReviewConvergePrompt(results []ReviewResult) string {
	var sb strings.Builder

	sb.WriteString("You are consolidating multiple code reviews into a single summary.\n\n")
	sb.WriteString("## Reviews to Consolidate\n\n")

	for i, r := range results {
		sb.WriteString(fmt.Sprintf("### Reviewer %d\n\n", i+1))

		if len(r.Issues) > 0 {
			sb.WriteString("Issues:\n")
			for _, issue := range r.Issues {
				sb.WriteString(fmt.Sprintf("- [%s] %s: %s (%s:%d)\n",
					issue.Severity,
					issue.Title,
					issue.Description,
					issue.File,
					issue.Line))
			}
		}

		if r.Summary != "" {
			sb.WriteString(fmt.Sprintf("\nSummary: %s\n", r.Summary))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Your Task\n\n")
	sb.WriteString("Consolidate these reviews into a single deduplicated list of issues.\n")
	sb.WriteString("- Merge duplicate or similar issues\n")
	sb.WriteString("- Order issues by severity (CRITICAL, HIGH, MEDIUM, LOW)\n")
	sb.WriteString("- Provide a concise overall summary\n\n")

	sb.WriteString("Output in this exact format:\n")
	sb.WriteString("<output>\n")
	sb.WriteString("REVIEW: {\n")
	sb.WriteString("  \"issues\": [\n")
	sb.WriteString("    {\n")
	sb.WriteString("      \"severity\": \"CRITICAL|HIGH|MEDIUM|LOW\",\n")
	sb.WriteString("      \"file\": \"path/to/file\",\n")
	sb.WriteString("      \"line\": 42,\n")
	sb.WriteString("      \"title\": \"Brief description\",\n")
	sb.WriteString("      \"description\": \"Detailed explanation\"\n")
	sb.WriteString("    }\n")
	sb.WriteString("  ],\n")
	sb.WriteString("  \"summary\": \"Overall assessment\"\n")
	sb.WriteString("}\n")
	sb.WriteString("</output>\n")

	return sb.String()
}

func mergeReviewsManually(results []ReviewResult) ReviewResult {
	// Simple manual merge: deduplicate by file+line+title
	seen := make(map[string]bool)
	var merged ReviewResult
	var summaries []string

	for _, r := range results {
		for _, issue := range r.Issues {
			key := fmt.Sprintf("%s:%d:%s", issue.File, issue.Line, issue.Title)
			if !seen[key] {
				seen[key] = true
				merged.Issues = append(merged.Issues, issue)
			}
		}
		if r.Summary != "" {
			summaries = append(summaries, r.Summary)
		}
	}

	// Sort by severity
	sortIssuesBySeverity(merged.Issues)

	if len(summaries) > 0 {
		merged.Summary = strings.Join(summaries, " | ")
	}

	return merged
}

// deduplicateAndSort removes duplicate issues and sorts by severity.
func deduplicateAndSort(result ReviewResult) ReviewResult {
	seen := make(map[string]bool)
	var deduped []ReviewIssue

	for _, issue := range result.Issues {
		key := fmt.Sprintf("%s:%d:%s", issue.File, issue.Line, issue.Title)
		if !seen[key] {
			seen[key] = true
			deduped = append(deduped, issue)
		}
	}

	sortIssuesBySeverity(deduped)

	return ReviewResult{
		Issues:  deduped,
		Summary: result.Summary,
	}
}

// sortIssuesBySeverity sorts issues by severity (CRITICAL > HIGH > MEDIUM > LOW).
func sortIssuesBySeverity(issues []ReviewIssue) {
	severityOrder := map[string]int{
		"CRITICAL": 0,
		"HIGH":     1,
		"MEDIUM":   2,
		"LOW":      3,
	}

	for i := 0; i < len(issues); i++ {
		for j := i + 1; j < len(issues); j++ {
			if severityOrder[issues[i].Severity] > severityOrder[issues[j].Severity] {
				issues[i], issues[j] = issues[j], issues[i]
			}
		}
	}
}

// PrintReviewResult prints a review result to stdout (used by show command).
func PrintReviewResult(result ReviewResult) {
	fmt.Println(TitleStyle.Render("Review Results"))
	fmt.Println()

	if len(result.Issues) == 0 {
		fmt.Println(SuccessStyle.Render("  No issues found!"))
	} else {
		fmt.Printf("  %s %d issue(s) found:\n\n", SubtitleStyle.Render("Found"), len(result.Issues))

		for _, issue := range result.Issues {
			var severityStyle func(string) string
			switch issue.Severity {
			case "CRITICAL":
				severityStyle = func(s string) string {
					return ErrorStyle.Render(s)
				}
			case "HIGH":
				severityStyle = func(s string) string {
					return StatusPendingStyle.Render(s)
				}
			case "MEDIUM":
				severityStyle = func(s string) string {
					return HighlightStyle.Render(s)
				}
			default:
				severityStyle = func(s string) string {
					return SubtitleStyle.Render(s)
				}
			}

			fmt.Printf("  %s %s\n",
				severityStyle(fmt.Sprintf("[%s]", issue.Severity)),
				NameStyle.Render(issue.Title))

			if issue.File != "" {
				location := issue.File
				if issue.Line > 0 {
					location = fmt.Sprintf("%s:%d", issue.File, issue.Line)
				}
				fmt.Printf("    %s %s\n", SubtitleStyle.Render("Location:"), location)
			}

			if issue.Description != "" {
				// Wrap description if too long
				desc := issue.Description
				if len(desc) > 80 {
					words := strings.Fields(desc)
					var lines []string
					var line string
					for _, word := range words {
						if len(line)+len(word)+1 > 76 {
							lines = append(lines, line)
							line = word
						} else if line == "" {
							line = word
						} else {
							line += " " + word
						}
					}
					if line != "" {
						lines = append(lines, line)
					}
					for _, l := range lines {
						fmt.Printf("    %s\n", l)
					}
				} else {
					fmt.Printf("    %s\n", desc)
				}
			}
			fmt.Println()
		}
	}

	if result.Summary != "" {
		fmt.Println(SubtitleStyle.Render("  Summary:"))
		fmt.Printf("    %s\n", result.Summary)
	}

	fmt.Println()
}

// LoadReviewResult loads a review result from the reviews directory.
func LoadReviewResult(taskID string) (*ReviewTaskResult, error) {
	reviewsDir, err := core.GetReviewsDir()
	if err != nil {
		return nil, err
	}

	resultFile := filepath.Join(reviewsDir, taskID+".json")
	data, err := os.ReadFile(resultFile)
	if err != nil {
		return nil, err
	}

	var result ReviewTaskResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return &result, nil
}
