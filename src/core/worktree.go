package core

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// WorktreeInfo holds information about a worktree's status
type WorktreeInfo struct {
	Name         string
	Path         string
	Branch       string
	CommitsAhead string
	HasChanges   bool
	IsRunning    bool
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

	// Check if the tracked process is still running
	if pid, ok := pids[worktreeName]; ok {
		info.IsRunning = IsProcessRunning(pid)
	}

	return info
}
