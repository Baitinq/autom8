package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/baitinq/autom8/src/core"
	"github.com/spf13/cobra"
)

var AcceptCmd = &cobra.Command{
	Use:     "accept <worktree-name>",
	Aliases: []string{"merge"},
	Short:   "Squash-merge a worktree branch into current branch",
	Long: `Accept and squash-merge a completed implementation from a worktree.

This command will:
  1. Auto-commit any uncommitted changes in the worktree
  2. Squash-merge the worktree's branch into your current branch
  3. Use the first commit message from the branch as the commit message

The worktree and branch are preserved. Use 'autom8 prune' to clean them up.`,
	Example: `  autom8 accept my-task-1`,
	Args:    cobra.ExactArgs(1),
	RunE:    runAccept,
}

func runAccept(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("worktree name required\nRun 'autom8 status' to see available worktrees")
	}

	worktreeName := args[0]

	gitRoot, err := core.GetGitRoot()
	if err != nil {
		return fmt.Errorf("error getting git root: %w", err)
	}

	worktreesDir, err := core.GetWorktreesDir()
	if err != nil {
		return fmt.Errorf("error getting worktrees dir: %w", err)
	}

	worktreePath := filepath.Join(worktreesDir, worktreeName)

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
		fmt.Println(SubtitleStyle.Render("Found uncommitted changes, auto-committing..."))

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
		fmt.Println(SuccessStyle.Render("Auto-committed successfully."))
	}

	fmt.Printf("Squash-merging branch '%s' into current branch...\n", HighlightStyle.Render(branchName))

	// Get the first commit message from the branch to use as the squash commit message
	mergeBaseCmd := exec.Command("git", "-C", gitRoot, "merge-base", "HEAD", branchName)
	mergeBaseOutput, err := mergeBaseCmd.Output()
	if err != nil {
		return fmt.Errorf("error getting merge base: %w", err)
	}
	mergeBase := strings.TrimSpace(string(mergeBaseOutput))
	if mergeBase == "" {
		return fmt.Errorf("could not determine merge base for branch '%s'", branchName)
	}

	// Get list of commits in chronological order (oldest first)
	revListCmd := exec.Command("git", "-C", gitRoot, "rev-list", "--reverse", fmt.Sprintf("%s..%s", mergeBase, branchName))
	revListOutput, err := revListCmd.Output()
	if err != nil {
		return fmt.Errorf("error getting commit list: %w", err)
	}
	commits := strings.Split(strings.TrimSpace(string(revListOutput)), "\n")
	if len(commits) == 0 || commits[0] == "" {
		return fmt.Errorf("no commits found on branch '%s'", branchName)
	}
	firstCommitHash := commits[0]

	// Get the first commit's message
	msgCmd := exec.Command("git", "-C", gitRoot, "log", "-1", "--format=%B", firstCommitHash)
	firstCommitOutput, err := msgCmd.Output()
	if err != nil {
		return fmt.Errorf("error getting first commit message: %w", err)
	}
	firstCommitMsg := string(firstCommitOutput)
	if strings.TrimSpace(firstCommitMsg) == "" {
		firstCommitMsg = fmt.Sprintf("Merge %s (autom8 accept)", branchName)
	}

	// Squash merge the branch into the current branch
	mergeCmd := exec.Command("git", "-C", gitRoot, "merge", "--squash", branchName)
	mergeOutput, err := mergeCmd.CombinedOutput()
	if err != nil {
		// Check for merge conflicts
		conflictFiles, conflictErr := getConflictingFiles(gitRoot)
		if conflictErr != nil || len(conflictFiles) == 0 {
			return fmt.Errorf("error merging branch: %w\n%s\nResolve conflicts manually, then run 'autom8 accept' again to clean up", err, string(mergeOutput))
		}

		// Attempt AI-assisted conflict resolution
		fmt.Println(SubtitleStyle.Render(fmt.Sprintf("Merge conflicts detected in %d file(s), attempting AI resolution...", len(conflictFiles))))

		// Load task info to provide context for conflict resolution
		tasks, _ := core.LoadTasks()
		taskIDs := make(map[string]struct{})
		for _, t := range tasks {
			taskIDs[t.ID] = struct{}{}
		}
		taskID, _ := core.TaskIDFromWorktree(worktreeName, taskIDs)
		var taskPrompt string
		for _, t := range tasks {
			if t.ID == taskID {
				taskPrompt = t.Prompt
				break
			}
		}

		if err := resolveConflictsWithAI(gitRoot, conflictFiles, taskPrompt); err != nil {
			// Abort the merge on failure
			abortCmd := exec.Command("git", "-C", gitRoot, "merge", "--abort")
			abortCmd.Run()
			return fmt.Errorf("AI conflict resolution failed: %w\nMerge has been aborted", err)
		}

		fmt.Println(SuccessStyle.Render("AI successfully resolved all conflicts."))
	} else {
		fmt.Printf("%s", string(mergeOutput))
	}

	// Commit the squashed changes with the first commit message
	commitCmd := exec.Command("git", "-C", gitRoot, "commit", "-F", "-")
	commitCmd.Stdin = strings.NewReader(firstCommitMsg)
	commitOutput, err := commitCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error committing squash merge: %w\n%s", err, string(commitOutput))
	}
	fmt.Printf("%s", string(commitOutput))

	// Mark the task as completed
	tasks, err := core.LoadTasks()
	if err != nil {
		fmt.Printf("%s could not load tasks to update status: %v\n", ErrorStyle.Render("Warning:"), err)
	} else {
		// Build task ID set for worktree name matching
		taskIDs := make(map[string]struct{})
		for _, t := range tasks {
			taskIDs[t.ID] = struct{}{}
		}

		// Extract task ID from worktree name using proper matching
		// This handles both independent ({task-name}-{instance}) and
		// dependent ({task-name}-{parent-instance}-{instance}) worktree names
		taskID, ok := core.TaskIDFromWorktree(worktreeName, taskIDs)
		if !ok {
			fmt.Printf("%s could not resolve task ID for worktree '%s'\n", ErrorStyle.Render("Warning:"), worktreeName)
		} else {
			for i, t := range tasks {
				if t.ID == taskID {
					tasks[i].Status = core.TaskStatusCompleted
					if err := core.SaveTasks(tasks); err != nil {
						fmt.Printf("%s could not save task status: %v\n", ErrorStyle.Render("Warning:"), err)
					} else {
						fmt.Printf("Marked task '%s' as completed.\n", taskID)
					}
					break
				}
			}
		}
	}

	logsDir, _ := core.GetLogsDir()
	statusPath := filepath.Join(logsDir, worktreeName, core.WorktreeStatusFile)
	if err := os.Remove(statusPath); err != nil && !os.IsNotExist(err) {
		fmt.Printf("%s could not clear worktree status: %v\n", ErrorStyle.Render("Warning:"), err)
	}

	fmt.Println()
	fmt.Println(SuccessStyle.Render(fmt.Sprintf("Successfully accepted worktree '%s'", worktreeName)))
	return nil
}

// getConflictingFiles returns the list of files with merge conflicts.
func getConflictingFiles(gitRoot string) ([]string, error) {
	cmd := exec.Command("git", "-C", gitRoot, "diff", "--name-only", "--diff-filter=U")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.TrimSpace(string(output))
	if lines == "" {
		return nil, nil
	}

	return strings.Split(lines, "\n"), nil
}

// resolveConflictsWithAI attempts to resolve merge conflicts using Claude AI.
func resolveConflictsWithAI(gitRoot string, conflictFiles []string, taskPrompt string) error {
	for _, file := range conflictFiles {
		fmt.Printf("  Resolving conflicts in %s...\n", HighlightStyle.Render(file))

		filePath := filepath.Join(gitRoot, file)
		content, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", file, err)
		}

		resolved, err := resolveFileConflictWithAI(file, string(content), taskPrompt)
		if err != nil {
			return fmt.Errorf("failed to resolve %s: %w", file, err)
		}

		if err := os.WriteFile(filePath, []byte(resolved), 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", file, err)
		}

		// Stage the resolved file
		addCmd := exec.Command("git", "-C", gitRoot, "add", file)
		if output, err := addCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to stage %s: %w\n%s", file, err, string(output))
		}
	}

	return nil
}

// resolveFileConflictWithAI uses Claude to resolve conflicts in a single file.
func resolveFileConflictWithAI(filename, content, taskPrompt string) (string, error) {
	prompt := buildConflictResolutionPrompt(filename, content, taskPrompt)

	cmd := exec.Command("claude", "-p", "-", "--output-format", "json")
	cmd.Stdin = strings.NewReader(prompt)

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("claude command failed: %w", err)
	}

	// Parse the JSON response
	var jsonResp struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(output, &jsonResp); err != nil {
		return "", fmt.Errorf("failed to parse claude response: %w", err)
	}

	// Extract the resolved file content from the response
	resolved := extractResolvedContent(jsonResp.Result)
	if resolved == "" {
		return "", fmt.Errorf("could not extract resolved content from AI response")
	}

	return resolved, nil
}

func buildConflictResolutionPrompt(filename, content, taskPrompt string) string {
	var sb strings.Builder

	sb.WriteString("You are resolving a git merge conflict. The worktree branch contains changes for a specific task, and those changes need to be merged into the main branch.\n\n")

	if taskPrompt != "" {
		sb.WriteString("## Task Context\n\n")
		sb.WriteString("The worktree was implementing this task:\n")
		sb.WriteString(taskPrompt)
		sb.WriteString("\n\n")
	}

	sb.WriteString("## Conflicting File\n\n")
	sb.WriteString(fmt.Sprintf("Filename: %s\n\n", filename))
	sb.WriteString("```\n")
	sb.WriteString(content)
	sb.WriteString("\n```\n\n")

	sb.WriteString("## Instructions\n\n")
	sb.WriteString("Resolve the merge conflict by:\n")
	sb.WriteString("1. Understanding what the worktree branch was trying to accomplish\n")
	sb.WriteString("2. Preserving the worktree's changes while integrating any necessary updates from the main branch\n")
	sb.WriteString("3. Removing ALL conflict markers (<<<<<<< HEAD, =======, >>>>>>> branch)\n")
	sb.WriteString("4. Ensuring the result is syntactically correct and functional\n\n")
	sb.WriteString("When in doubt, prefer the worktree's changes since they represent the new feature being merged.\n\n")
	sb.WriteString("IMPORTANT: Your response MUST include the complete resolved file content wrapped in these tags:\n")
	sb.WriteString("<resolved-file>\n")
	sb.WriteString("(complete file content here)\n")
	sb.WriteString("</resolved-file>\n\n")
	sb.WriteString("Do NOT include any explanations inside the tags - only the raw file content.\n")

	return sb.String()
}

func extractResolvedContent(response string) string {
	start := strings.Index(response, "<resolved-file>")
	if start == -1 {
		return ""
	}
	start += len("<resolved-file>")

	end := strings.Index(response[start:], "</resolved-file>")
	if end == -1 {
		return ""
	}

	content := response[start : start+end]
	// Trim the leading newline (after opening tag) but keep trailing newline for proper file formatting
	content = strings.TrimPrefix(content, "\n")
	// Ensure file ends with exactly one newline (standard for source files)
	content = strings.TrimRight(content, "\n") + "\n"
	return content
}
