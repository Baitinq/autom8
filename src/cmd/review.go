package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/baitinq/autom8/src/core"
	"github.com/spf13/cobra"
)

var (
	reviewNumInstances int
)

var ReviewCmd = &cobra.Command{
	Use:   "review [PR#|branch]",
	Short: "Run parallel code reviewers on a PR or branch",
	Long: `Spawn N parallel code reviewers to analyze a PR or branch.

If no argument is provided, reviews the current branch against main.
You can specify a PR number or branch name to review.

Each reviewer independently analyzes the changes and outputs findings.
The results are then converged into a deduplicated summary of issues.`,
	Example: `  # Review current branch vs main
  autom8 review

  # Review a specific PR
  autom8 review 123

  # Review a specific branch
  autom8 review feature-branch

  # Run 3 parallel reviewers
  autom8 review -n 3`,
	Args: cobra.MaximumNArgs(1),
	RunE: runReview,
}

func init() {
	ReviewCmd.Flags().IntVarP(&reviewNumInstances, "instances", "n", 1, "Number of parallel reviewers")
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

	// Load configuration for reviewer tool/model
	cfg, err := core.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Use reviewer config (defaults to codex via LoadConfig)
	reviewerCfg := cfg.Reviewer

	fmt.Println(TitleStyle.Render("Code Review"))
	fmt.Println()
	fmt.Printf("  %s %s\n", SubtitleStyle.Render("Target:"), HighlightStyle.Render(reviewTarget))
	fmt.Printf("  %s %d\n", SubtitleStyle.Render("Reviewers:"), reviewNumInstances)
	fmt.Printf("  %s %s\n", SubtitleStyle.Render("Tool:"), reviewerCfg.Tool)
	fmt.Println()
	fmt.Println(SubtitleStyle.Render("Running reviewers..."))
	fmt.Println()

	// Create temp directory for review outputs
	tempDir, err := os.MkdirTemp("", "autom8-review-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Load the code-reviewer agent template
	codeReviewerTemplate, err := loadAgentTemplate("code-reviewer")
	if err != nil {
		codeReviewerTemplate = ""
	}

	// Spawn N parallel reviewers
	var wg sync.WaitGroup
	results := make([]ReviewResult, reviewNumInstances)
	errors := make([]error, reviewNumInstances)

	for i := 0; i < reviewNumInstances; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			// Build reviewer prompt
			prompt := buildCodeReviewPrompt(codeReviewerTemplate, reviewTarget)

			// Create output file for this reviewer
			outputFile := filepath.Join(tempDir, fmt.Sprintf("review-%d.json", idx))

			// Run the reviewer
			result, err := runCodeReviewer(reviewerCfg, prompt, gitRoot, outputFile)
			if err != nil {
				errors[idx] = err
				return
			}
			results[idx] = result

			fmt.Printf("  %s Reviewer %d completed (%d issues found)\n",
				SuccessStyle.Render("✓"),
				idx+1,
				len(result.Issues))
		}(i)
	}

	wg.Wait()

	// Check for errors
	var successfulResults []ReviewResult
	for i := 0; i < reviewNumInstances; i++ {
		if errors[i] != nil {
			fmt.Printf("  %s Reviewer %d failed: %v\n",
				ErrorStyle.Render("✗"),
				i+1,
				errors[i])
		} else {
			successfulResults = append(successfulResults, results[i])
		}
	}

	if len(successfulResults) == 0 {
		return fmt.Errorf("all reviewers failed")
	}

	fmt.Println()
	fmt.Println(SubtitleStyle.Render("Consolidating reviews..."))
	fmt.Println()

	// Converge the results into a single deduplicated summary
	consolidated := convergeReviews(successfulResults, reviewerCfg, gitRoot)

	// Print the consolidated review
	printConsolidatedReview(consolidated)

	return nil
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

func runCodeReviewer(cfg core.ToolConfig, prompt, workDir, outputFile string) (ReviewResult, error) {
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
	result, err := parseReviewOutput(string(output))
	if err != nil {
		return ReviewResult{}, err
	}

	// Write parsed result to output file
	resultJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return result, nil // Return result even if we can't write the file
	}
	if writeErr := os.WriteFile(outputFile, resultJSON, 0644); writeErr != nil {
		// Log but don't fail - the result is still valid
		fmt.Fprintf(os.Stderr, "Warning: failed to write review output to %s: %v\n", outputFile, writeErr)
	}

	return result, nil
}

func parseReviewOutput(output string) (ReviewResult, error) {
	// Try to parse JSON response from Claude's --output-format json
	var jsonResp struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal([]byte(output), &jsonResp); err == nil {
		output = jsonResp.Result
	}

	// Look for <output>REVIEW: {...}</output> pattern
	var result ReviewResult

	start := strings.Index(output, "<output>")
	end := strings.LastIndex(output, "</output>")

	if start != -1 && end != -1 && end > start {
		content := strings.TrimSpace(output[start+len("<output>") : end])

		// Check if it starts with "REVIEW:"
		if strings.HasPrefix(content, "REVIEW:") {
			content = strings.TrimPrefix(content, "REVIEW:")
			content = strings.TrimSpace(content)
		}

		// Parse the JSON
		if err := json.Unmarshal([]byte(content), &result); err != nil {
			// If JSON parsing fails, try to extract something useful
			result.Summary = content
		}
	} else {
		// Fallback: treat the whole output as a summary
		result.Summary = strings.TrimSpace(output)
	}

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

func printConsolidatedReview(result ReviewResult) {
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
