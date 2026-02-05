package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/baitinq/autom8/src/core"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

var importYesFlag bool

var ImportCmd = &cobra.Command{
	Use:   "import [file]",
	Short: "Import tasks from any text format using AI",
	Long: `Import tasks from any text format (markdown, bullets, prose, etc.) using AI.

The AI will parse the input, extract tasks, infer dependencies, and avoid
duplicating tasks that already exist.

If no file is specified, reads from stdin.`,
	Example: `  # Import from file
  autom8 import TODO.md
  autom8 import tasks.txt

  # Import from stdin
  cat tasks.md | autom8 import -y
  jira-cli list | autom8 import -y`,
	Args: cobra.MaximumNArgs(1),
	RunE: runImport,
}

func init() {
	ImportCmd.Flags().BoolVarP(&importYesFlag, "yes", "y", false, "Skip confirmation prompt")
}

// ImportedTask represents a task parsed by the AI from the input.
type ImportedTask struct {
	Name      string   `json:"name"`
	Prompt    string   `json:"prompt"`
	Criteria  []string `json:"criteria,omitempty"`
	DependsOn string   `json:"depends_on,omitempty"`
}

// ImportResponse represents the AI's response when parsing tasks.
type ImportResponse struct {
	Tasks   []ImportedTask `json:"tasks"`
	Skipped []string       `json:"skipped,omitempty"` // IDs of skipped existing tasks
}

func runImport(cmd *cobra.Command, args []string) error {
	// Check git repo first
	if _, err := core.GetGitRoot(); err != nil {
		return err
	}

	// Read input (from file or stdin)
	var input string
	if len(args) > 0 {
		// Read from file
		data, err := os.ReadFile(args[0])
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}
		input = string(data)
	} else {
		// Read from stdin
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) != 0 {
			return fmt.Errorf("no input provided (pipe content or specify a file)")
		}
		reader := bufio.NewReader(os.Stdin)
		data, err := io.ReadAll(reader)
		if err != nil {
			return fmt.Errorf("failed to read stdin: %w", err)
		}
		input = string(data)
	}

	if strings.TrimSpace(input) == "" {
		return fmt.Errorf("input is empty")
	}

	// Load existing tasks
	existingTasks, err := core.LoadTasks()
	if err != nil {
		return fmt.Errorf("failed to load existing tasks: %w", err)
	}

	// Build the prompt for AI
	aiPrompt := buildImportPrompt(input, existingTasks)

	// Call Claude AI
	fmt.Println(SubtitleStyle.Render("Analyzing input with AI..."))

	claudeCmd := exec.Command("claude", "-p", "-", "--output-format", "json")
	claudeCmd.Stdin = strings.NewReader(aiPrompt)

	output, err := claudeCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to run AI analysis: %w", err)
	}

	// Parse AI response
	importResp, err := parseImportResponse(string(output))
	if err != nil {
		return fmt.Errorf("failed to parse AI response: %w", err)
	}

	if len(importResp.Tasks) == 0 {
		fmt.Println(SubtitleStyle.Render("No new tasks found to import."))
		if len(importResp.Skipped) > 0 {
			fmt.Printf("\nSkipped %d existing task(s):\n", len(importResp.Skipped))
			for _, id := range importResp.Skipped {
				fmt.Printf("  - %s\n", id)
			}
		}
		return nil
	}

	// Validate imported tasks
	validTasks, invalidTasks := validateImportedTasks(importResp.Tasks, existingTasks)

	if len(validTasks) == 0 {
		fmt.Println(SubtitleStyle.Render("No valid tasks to import after validation."))
		return nil
	}

	// Show summary
	var completeTasks, incompleteTasks []ImportedTask
	for _, t := range validTasks {
		if strings.TrimSpace(t.Prompt) == "" || len(t.Criteria) == 0 {
			incompleteTasks = append(incompleteTasks, t)
		} else {
			completeTasks = append(completeTasks, t)
		}
	}

	fmt.Printf("\n%s\n", TitleStyle.Render(fmt.Sprintf("Importing %d task(s):", len(validTasks))))
	for _, t := range completeTasks {
		dep := ""
		if t.DependsOn != "" {
			dep = fmt.Sprintf(" (depends on: %s)", t.DependsOn)
		}
		fmt.Printf("  - %s%s\n", NameStyle.Render(t.Name), dep)
	}
	for _, t := range incompleteTasks {
		dep := ""
		if t.DependsOn != "" {
			dep = fmt.Sprintf(" (depends on: %s)", t.DependsOn)
		}
		fmt.Printf("  - %s%s %s\n", NameStyle.Render(t.Name), dep, SubtitleStyle.Render("[needs definition - use 'autom8 edit']"))
	}

	if len(invalidTasks) > 0 {
		fmt.Printf("\n%s\n", SubtitleStyle.Render(fmt.Sprintf("Skipped %d invalid task(s):", len(invalidTasks))))
		for _, reason := range invalidTasks {
			fmt.Printf("  - %s\n", reason)
		}
	}

	if len(importResp.Skipped) > 0 {
		fmt.Printf("\n%s\n", SubtitleStyle.Render(fmt.Sprintf("Skipped %d existing task(s):", len(importResp.Skipped))))
		for _, id := range importResp.Skipped {
			fmt.Printf("  - %s\n", id)
		}
	}

	// Confirm with user (skip if -y flag or stdin was piped)
	stdinWasPiped := len(args) == 0 // If no file arg, we read from stdin
	if !importYesFlag && !stdinWasPiped {
		var confirm bool
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Proceed with import?").
					Value(&confirm),
			),
		).WithTheme(huh.ThemeDracula())

		if err := form.Run(); err != nil {
			if err == huh.ErrUserAborted {
				fmt.Println("\nAborted.")
				return nil
			}
			return err
		}

		if !confirm {
			fmt.Println("\nAborted.")
			return nil
		}
	} else if !importYesFlag && stdinWasPiped {
		// Stdin was piped but no -y flag - require -y for safety
		return fmt.Errorf("when reading from stdin, use -y to confirm import")
	}

	// Convert to core.Task and append
	for _, t := range validTasks {
		task := core.Task{
			ID:                   t.Name,
			Prompt:               t.Prompt,
			VerificationCriteria: t.Criteria,
			DependsOn:            t.DependsOn,
			CreatedAt:            time.Now(),
			Status:               core.TaskStatusPending,
		}
		existingTasks = append(existingTasks, task)
	}

	// Save tasks
	if err := core.SaveTasks(existingTasks); err != nil {
		return fmt.Errorf("failed to save tasks: %w", err)
	}

	fmt.Println()
	fmt.Println(SuccessStyle.Render(fmt.Sprintf("Successfully imported %d task(s)!", len(validTasks))))
	return nil
}

func buildImportPrompt(input string, existingTasks []core.Task) string {
	var sb strings.Builder

	sb.WriteString("You are a task parser for the autom8 CLI tool. Parse the following input and extract tasks.\n\n")

	sb.WriteString("## Input to Parse\n\n")
	sb.WriteString("```\n")
	sb.WriteString(input)
	sb.WriteString("\n```\n\n")

	sb.WriteString("## Existing Tasks (do not re-import these)\n\n")
	if len(existingTasks) == 0 {
		sb.WriteString("(none)\n\n")
	} else {
		for _, t := range existingTasks {
			sb.WriteString(fmt.Sprintf("- **%s**: %s\n", t.ID, core.Truncate(t.Prompt, 80)))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Your Task\n\n")
	sb.WriteString("1. Parse the input (it could be markdown, bullets, numbered lists, prose, Jira output, etc.)\n")
	sb.WriteString("2. Extract discrete tasks from the content\n")
	sb.WriteString("3. For each task, classify it as WELL-DEFINED or VAGUE:\n\n")
	sb.WriteString("   **WELL-DEFINED tasks** have clear descriptions explaining what to implement:\n")
	sb.WriteString("   - Set **name**, **prompt** (clear instruction), and **criteria** (success criteria)\n\n")
	sb.WriteString("   **VAGUE tasks** are brief/unclear items (e.g., just 'Add login', 'Fix bug', 'Update tests'):\n")
	sb.WriteString("   - Set **name** only (derived from the brief text)\n")
	sb.WriteString("   - Leave **prompt** empty (empty string \"\")\n")
	sb.WriteString("   - Leave **criteria** empty (empty array [])\n")
	sb.WriteString("   - The user will need to flesh these out later with 'autom8 edit'\n\n")
	sb.WriteString("4. For each task field:\n")
	sb.WriteString("   - **name**: A kebab-case identifier (max 50 chars, alphanumeric with dashes/underscores)\n")
	sb.WriteString("   - **prompt**: What should be implemented (clear instruction) - LEAVE EMPTY FOR VAGUE TASKS\n")
	sb.WriteString("   - **criteria**: Verification criteria - LEAVE EMPTY FOR VAGUE TASKS\n")
	sb.WriteString("   - **depends_on**: Another task name if there's a logical dependency (optional)\n")
	sb.WriteString("5. Skip tasks that seem to match existing ones (fuzzy match on intent/meaning)\n")
	sb.WriteString("6. Infer dependencies when logical (e.g., 'after X is done', 'requires Y', ordering)\n\n")

	sb.WriteString("## Output Format\n\n")
	sb.WriteString("Output ONLY a JSON object inside <output> tags with this structure:\n\n")
	sb.WriteString("```json\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"tasks\": [\n")
	sb.WriteString("    {\n")
	sb.WriteString("      \"name\": \"kebab-case-name\",\n")
	sb.WriteString("      \"prompt\": \"Clear implementation instruction\",\n")
	sb.WriteString("      \"criteria\": [\"criterion 1\", \"criterion 2\"],\n")
	sb.WriteString("      \"depends_on\": \"other-task-name\"\n")
	sb.WriteString("    }\n")
	sb.WriteString("  ],\n")
	sb.WriteString("  \"skipped\": [\"existing-task-id-1\"]\n")
	sb.WriteString("}\n")
	sb.WriteString("```\n\n")

	sb.WriteString("IMPORTANT:\n")
	sb.WriteString("- Output ONLY the JSON inside <output></output> tags, no other text\n")
	sb.WriteString("- Task names must be valid: start with alphanumeric, contain only alphanumeric/dashes/underscores, max 50 chars\n")
	sb.WriteString("- Dependencies must reference other tasks in the same import OR existing tasks\n")
	sb.WriteString("- If no tasks are found, return {\"tasks\": [], \"skipped\": []}\n")
	sb.WriteString("- BE CONSERVATIVE: For vague/brief items, set name only with empty prompt and criteria. Only fill in prompt/criteria for well-defined tasks with clear descriptions.\n\n")

	sb.WriteString("<output>\n")

	return sb.String()
}

func parseImportResponse(response string) (*ImportResponse, error) {
	// Try to parse JSON response from Claude's --output-format json
	var jsonResp struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal([]byte(response), &jsonResp); err == nil {
		response = jsonResp.Result
	}

	// Look for <output>...</output> tags
	start := strings.Index(response, "<output>")
	end := strings.LastIndex(response, "</output>")

	var jsonStr string
	if start != -1 && end != -1 && end > start {
		jsonStr = strings.TrimSpace(response[start+len("<output>") : end])
	} else {
		// Try to find JSON object boundaries directly
		jsonStart := strings.Index(response, "{")
		jsonEnd := strings.LastIndex(response, "}")
		if jsonStart != -1 && jsonEnd != -1 && jsonEnd > jsonStart {
			jsonStr = response[jsonStart : jsonEnd+1]
		} else {
			jsonStr = strings.TrimSpace(response)
		}
	}

	// Handle case where JSON might be wrapped in code blocks
	jsonStr = strings.TrimPrefix(jsonStr, "```json")
	jsonStr = strings.TrimPrefix(jsonStr, "```")
	jsonStr = strings.TrimSuffix(jsonStr, "```")
	jsonStr = strings.TrimSpace(jsonStr)

	// Find the actual JSON object boundaries (in case there's extra text around it)
	if !strings.HasPrefix(jsonStr, "{") {
		if idx := strings.Index(jsonStr, "{"); idx != -1 {
			jsonStr = jsonStr[idx:]
		}
	}
	if lastBrace := strings.LastIndex(jsonStr, "}"); lastBrace != -1 && lastBrace < len(jsonStr)-1 {
		jsonStr = jsonStr[:lastBrace+1]
	}

	var importResp ImportResponse
	if err := json.Unmarshal([]byte(jsonStr), &importResp); err != nil {
		preview := jsonStr
		if len(preview) > 200 {
			preview = preview[:200]
		}
		return nil, fmt.Errorf("failed to parse JSON: %w (response: %s)", err, preview)
	}

	return &importResp, nil
}

func validateImportedTasks(tasks []ImportedTask, existingTasks []core.Task) ([]ImportedTask, []string) {
	var valid []ImportedTask
	var invalid []string

	// Build set of existing task IDs
	existingIDs := make(map[string]bool)
	for _, t := range existingTasks {
		existingIDs[t.ID] = true
	}

	// Also track IDs we're adding in this import
	addedIDs := make(map[string]bool)

	for _, t := range tasks {
		// Validate name
		if err := core.ValidateTaskName(t.Name); err != nil {
			invalid = append(invalid, fmt.Sprintf("%s: %s", t.Name, err.Error()))
			continue
		}

		// Check for duplicates against existing
		if existingIDs[t.Name] {
			invalid = append(invalid, fmt.Sprintf("%s: already exists", t.Name))
			continue
		}

		// Check for duplicates within this import
		if addedIDs[t.Name] {
			invalid = append(invalid, fmt.Sprintf("%s: duplicate in import", t.Name))
			continue
		}

		// Validate dependency exists (either existing or in this import)
		if t.DependsOn != "" {
			if !existingIDs[t.DependsOn] && !addedIDs[t.DependsOn] {
				// Check if it's coming later in the import
				found := false
				for _, other := range tasks {
					if other.Name == t.DependsOn {
						found = true
						break
					}
				}
				if !found {
					invalid = append(invalid, fmt.Sprintf("%s: dependency '%s' not found", t.Name, t.DependsOn))
					continue
				}
			}
		}

		// Note: We allow empty prompts and criteria for vague tasks.
		// The user will need to complete them with 'autom8 edit' before implementing.

		valid = append(valid, t)
		addedIDs[t.Name] = true
	}

	return valid, invalid
}

