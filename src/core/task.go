package core

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	Autom8Dir = ".autom8"
	TasksFile = "tasks.json"
	PidsFile  = "pids.json"
)

type Task struct {
	ID                   string    `json:"id"`
	Prompt               string    `json:"prompt"`
	VerificationCriteria []string  `json:"verification_criteria"`
	DependsOn            string    `json:"depends_on,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
	Status               string    `json:"status"`
	Winner               string    `json:"winner,omitempty"` // Winning worktree name from converge
}

func GetGitRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("must be run inside a git repository")
	}
	return strings.TrimSpace(string(output)), nil
}

func GetAutom8Dir() (string, error) {
	gitRoot, err := GetGitRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(gitRoot, Autom8Dir), nil
}

func EnsureAutom8Dir() (string, error) {
	dir, err := GetAutom8Dir()
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	return dir, nil
}

func LoadTasks() ([]Task, error) {
	dir, err := GetAutom8Dir()
	if err != nil {
		return nil, err
	}

	tasksPath := filepath.Join(dir, TasksFile)

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

func SaveTasks(tasks []Task) error {
	dir, err := EnsureAutom8Dir()
	if err != nil {
		return err
	}

	tasksPath := filepath.Join(dir, TasksFile)

	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(tasksPath, data, 0644)
}

// MaxTaskNameLength is the maximum allowed length for task names.
const MaxTaskNameLength = 50

// taskNameRegex validates task names: alphanumeric, dashes, and underscores only.
var taskNameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// ValidateTaskName checks that a task name follows the naming rules:
// - No spaces
// - Alphanumeric with dashes and underscores allowed
// - Reasonable max length (50 chars)
// - Must start with alphanumeric character
func ValidateTaskName(name string) error {
	if name == "" {
		return fmt.Errorf("task name cannot be empty")
	}
	if len(name) > MaxTaskNameLength {
		return fmt.Errorf("task name cannot exceed %d characters", MaxTaskNameLength)
	}
	if strings.Contains(name, " ") {
		return fmt.Errorf("task name cannot contain spaces")
	}
	if !taskNameRegex.MatchString(name) {
		return fmt.Errorf("task name must start with alphanumeric and contain only alphanumeric characters, dashes, and underscores")
	}
	return nil
}

// IsTaskNameUnique checks if the given name is unique among existing tasks.
// If excludeID is non-empty, that task ID is excluded from the check (useful for editing).
func IsTaskNameUnique(tasks []Task, name string, excludeID string) bool {
	for _, t := range tasks {
		if t.ID == name && t.ID != excludeID {
			return false
		}
	}
	return true
}

// Truncate truncates a string to maxLen characters, replacing newlines with spaces.
func Truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// BaseTaskIDFromWorktree extracts the task ID from a worktree name.
// Worktree names follow patterns like:
//   - Independent: {task-name}-{instance}  (e.g., "my-task-1")
//   - Dependent: {task-name}-{parent-instance}-{instance}  (e.g., "my-task-2-1")
//
// This function iterates through the name parts and matches against known task IDs
// to correctly identify the task even for dependent tasks with multiple numeric suffixes.
func BaseTaskIDFromWorktree(name string, taskIDs map[string]struct{}) (string, bool) {
	parts := strings.Split(name, "-")
	if len(parts) < 2 {
		return "", false
	}
	// Last part must be numeric (instance number)
	if _, err := strconv.Atoi(parts[len(parts)-1]); err != nil {
		return "", false
	}
	// Try to match task IDs by progressively removing numeric suffixes
	for i := len(parts) - 1; i >= 1; i-- {
		if _, err := strconv.Atoi(parts[i]); err != nil {
			// Hit a non-numeric part, stop iterating
			break
		}
		candidate := strings.Join(parts[:i], "-")
		if _, ok := taskIDs[candidate]; ok {
			return candidate, true
		}
	}
	return "", false
}
