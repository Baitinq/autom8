package core

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	WorkerPidFile      = "worker.pid"
	WorktreeStatusFile = "status.json"
)

// WorktreeStatus represents the status of a worktree stored in status.json
type WorktreeStatus struct {
	Status       WorktreePhase `json:"status"`                  // "implementing", "reviewing", "ready", "idle"
	Iteration    int           `json:"iteration,omitempty"`     // Current implementation iteration (1-based)
	FixIteration int           `json:"fix_iteration,omitempty"` // Current review fix iteration (1-based)
	CompletedAt  time.Time     `json:"completed_at,omitempty"`
}

type WorktreePhase string

const (
	WorktreePhaseImplementing WorktreePhase = "implementing"
	WorktreePhaseReviewing    WorktreePhase = "reviewing"
	WorktreePhaseReady        WorktreePhase = "ready"
	WorktreePhaseIdle         WorktreePhase = "idle"
)

// WorktreeInfo holds information about a worktree's status
type WorktreeInfo struct {
	Name         string
	Path         string
	Branch       string
	CommitsAhead string
	HasChanges   bool
	IsRunning    bool
	Phase        WorktreePhase // Current phase: "implementing", "reviewing", "ready", "idle"
	Iteration    int           // Implementation iteration count (when Phase == "implementing")
	FixIteration int           // Review fix iteration count (when Phase == "reviewing")
}

func LoadPids() (map[string]int, error) {
	dir, err := GetAutom8Dir()
	if err != nil {
		return make(map[string]int), nil
	}

	pidsPath := filepath.Join(dir, PidsFile)
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

func SavePids(pids map[string]int) error {
	dir, err := EnsureAutom8Dir()
	if err != nil {
		return err
	}

	pidsPath := filepath.Join(dir, PidsFile)
	data, err := json.MarshalIndent(pids, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(pidsPath, data, 0644)
}

func SavePid(worktreeName string, pid int) {
	pids, _ := LoadPids()
	pids[worktreeName] = pid
	SavePids(pids)
}

func IsProcessRunning(pid int) bool {
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

func GetWorktreeInfo(worktreesDir, worktreeName string, pids map[string]int) WorktreeInfo {
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

	// Check if the worker is running by looking for the PID file in logs directory
	// (PID file is stored in .autom8/logs/<worktreeName>/worker.pid to avoid git status noise)
	autom8Dir, err := GetAutom8Dir()
	if err == nil {
		pidFilePath := filepath.Join(autom8Dir, "logs", worktreeName, WorkerPidFile)
		if pidData, err := os.ReadFile(pidFilePath); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(pidData))); err == nil {
				info.IsRunning = IsProcessRunning(pid)
			}
		}
	}

	// Fall back to centralized pids.json if no worker.pid file found
	if !info.IsRunning {
		if pid, ok := pids[worktreeName]; ok {
			info.IsRunning = IsProcessRunning(pid)
		}
	}

	// Read worktree status file to get phase and iteration info
	if status, err := ReadWorktreeStatus(worktreeName); err == nil {
		info.Phase = status.Status
		info.Iteration = status.Iteration
		info.FixIteration = status.FixIteration
	}

	return info
}

func GetWorktreesDir() (string, error) {
	autom8Dir, err := GetAutom8Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(autom8Dir, "worktrees"), nil
}

// ListWorktreesByTask groups worktrees by task ID.
func ListWorktreesByTask(worktreesDir string, taskIDs map[string]struct{}, pids map[string]int) map[string][]WorktreeInfo {
	worktreesByTask := make(map[string][]WorktreeInfo)
	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		return worktreesByTask
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		worktreeName := entry.Name()
		taskID, ok := TaskIDFromWorktree(worktreeName, taskIDs)
		if !ok {
			continue
		}
		info := GetWorktreeInfo(worktreesDir, worktreeName, pids)
		worktreesByTask[taskID] = append(worktreesByTask[taskID], info)
	}

	return worktreesByTask
}

// ReadWorktreeStatus reads the status file for a worktree from .autom8/logs/<worktreeName>/status.json
func ReadWorktreeStatus(worktreeName string) (*WorktreeStatus, error) {
	autom8Dir, err := GetAutom8Dir()
	if err != nil {
		return nil, err
	}

	statusPath := filepath.Join(autom8Dir, "logs", worktreeName, WorktreeStatusFile)
	data, err := os.ReadFile(statusPath)
	if err != nil {
		return nil, err
	}

	var status WorktreeStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, err
	}
	return &status, nil
}

// WriteWorktreeStatus writes the status file for a worktree to .autom8/logs/<worktreeName>/status.json
func WriteWorktreeStatus(worktreeName string, status *WorktreeStatus) error {
	autom8Dir, err := GetAutom8Dir()
	if err != nil {
		return err
	}

	logsDir := filepath.Join(autom8Dir, "logs", worktreeName)
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return err
	}

	statusPath := filepath.Join(logsDir, WorktreeStatusFile)
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statusPath, data, 0644)
}
