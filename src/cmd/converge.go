package cmd

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/baitinq/autom8/src/core"
)

// ConvergeResult holds the result of a convergence operation.
type ConvergeResult struct {
	Winner    string
	Reasoning string
	Error     error
}

// ConvergeTask runs convergence analysis on a task's worktrees and returns the result.
// This is the core convergence logic used by auto-converge.
// For single worktree, it returns that worktree as the trivial winner.
func ConvergeTask(task core.Task, worktrees []core.WorktreeInfo, gitRoot string) ConvergeResult {
	if len(worktrees) == 0 {
		return ConvergeResult{Error: fmt.Errorf("no worktrees to converge")}
	}

	// Single worktree: trivially pick it as the winner
	if len(worktrees) == 1 {
		return ConvergeResult{
			Winner:    worktrees[0].Name,
			Reasoning: "Single worktree, automatically selected",
		}
	}

	// Build the converge prompt
	convergePrompt := buildConvergePrompt(task, worktrees, gitRoot)

	// Run claude to analyze (use stdin to avoid "argument list too long" error)
	claudeCmd := exec.Command("claude", "-p", "-", "--output-format", "json")
	claudeCmd.Dir = gitRoot
	claudeCmd.Stdin = strings.NewReader(convergePrompt)

	output, err := claudeCmd.Output()
	if err != nil {
		return ConvergeResult{Error: fmt.Errorf("failed to run AI analysis: %w", err)}
	}

	// Parse the response to extract the winner and reasoning
	winner, reasoning := parseConvergeResponse(string(output), worktrees)
	if winner == "" {
		return ConvergeResult{Error: fmt.Errorf("could not determine a winner from AI response")}
	}

	return ConvergeResult{
		Winner:    winner,
		Reasoning: reasoning,
	}
}

func buildConvergePrompt(task core.Task, worktrees []core.WorktreeInfo, gitRoot string) string {
	var sb strings.Builder

	sb.WriteString("You are evaluating multiple implementations of the same task to determine which is best.\n\n")

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

	sb.WriteString("## Implementations\n\n")
	sb.WriteString("Below are the diffs for each implementation worktree:\n\n")

	for _, wt := range worktrees {
		sb.WriteString(fmt.Sprintf("### Worktree: %s\n\n", wt.Name))

		// Get the diff for this worktree (against its upstream)
		diffCmd := exec.Command("git", "-C", wt.Path, "diff", "@{u}...HEAD")
		diffOutput, err := diffCmd.Output()
		if err != nil {
			sb.WriteString("(could not get diff)\n\n")
		} else if len(diffOutput) == 0 {
			sb.WriteString("(no changes)\n\n")
		} else {
			// Truncate very large diffs
			diff := string(diffOutput)
			if len(diff) > 50000 {
				diff = diff[:50000] + "\n... (truncated)"
			}
			sb.WriteString("```diff\n")
			sb.WriteString(diff)
			sb.WriteString("\n```\n\n")
		}
	}

	sb.WriteString("## Your Task\n\n")
	sb.WriteString("Analyze each implementation and determine which one best satisfies the task requirements and verification criteria.\n\n")
	sb.WriteString("Consider:\n")
	sb.WriteString("- Correctness: Does the implementation actually solve the task?\n")
	sb.WriteString("- Completeness: Are all verification criteria met?\n")
	sb.WriteString("- Code quality: Is the code clean, readable, and maintainable?\n")
	sb.WriteString("- Simplicity: Is the solution appropriately simple without over-engineering?\n\n")
	sb.WriteString("IMPORTANT: Your response MUST include the exact worktree name of the winner AND a brief reasoning summary in this format:\n")
	sb.WriteString("<output>WINNER: <worktree-name></output>\n")
	sb.WriteString("<output>REASONING: <1-2 sentence summary></output>\n\n")
	sb.WriteString("For example:\n")
	sb.WriteString("<output>WINNER: my-task-1</output>\n")
	sb.WriteString("<output>REASONING: This implementation correctly handles all edge cases and has cleaner code structure.</output>\n\n")
	sb.WriteString("Explain your full reasoning before declaring the winner, then provide the summary.\n")

	return sb.String()
}

func parseConvergeResponse(response string, worktrees []core.WorktreeInfo) (string, string) {
	// Try to parse JSON response first
	var jsonResp struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal([]byte(response), &jsonResp); err == nil {
		response = jsonResp.Result
	}

	var winner, reasoning string

	// Look for "<output>WINNER: name</output>" pattern
	if start := strings.Index(response, "<output>WINNER:"); start != -1 {
		start += len("<output>WINNER:")
		if end := strings.Index(response[start:], "</output>"); end != -1 {
			candidate := strings.TrimSpace(response[start : start+end])
			for _, wt := range worktrees {
				if wt.Name == candidate {
					winner = candidate
					break
				}
			}
		}
	}

	// Look for "<output>REASONING: summary</output>" pattern
	if start := strings.Index(response, "<output>REASONING:"); start != -1 {
		start += len("<output>REASONING:")
		if end := strings.Index(response[start:], "</output>"); end != -1 {
			reasoning = strings.TrimSpace(response[start : start+end])
		}
	}

	return winner, reasoning
}
