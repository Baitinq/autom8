package cmd

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"syscall"
	"time"

	"github.com/baitinq/autom8/src/core"
	"github.com/spf13/cobra"
)

var LogsCmd = &cobra.Command{
	Use:   "logs <worktree-name>",
	Short: "Stream real-time logs from a worktree's implementation/review agents",
	Long: `Stream real-time logs from a worktree's implementation and review agents.

This command shows the iteration and review logs as they are written, similar to
'tail -f'. It automatically detects when new iteration files are created and
switches to following them.

Log files streamed:
  - iteration-N.log: Implementation iteration logs
  - review-iteration-N.log: Review iteration logs

Press Ctrl+C to stop streaming.`,
	Example: `  autom8 logs my-task-1
  autom8 logs add-feature-2`,
	Args: cobra.ExactArgs(1),
	RunE: runLogs,
}

// logFile represents a log file with its parsed iteration number
type logFile struct {
	path      string
	name      string
	iteration int
	isReview  bool
	modTime   time.Time
}

// Patterns to match log files
var (
	iterationPattern       = regexp.MustCompile(`^iteration-(\d+)\.log$`)
	reviewIterationPattern = regexp.MustCompile(`^review-iteration-(\d+)\.log$`)
)

func runLogs(cmd *cobra.Command, args []string) error {
	worktreeName := args[0]

	// Get the logs directory
	baseLogsDir, err := core.GetLogsDir()
	if err != nil {
		return err
	}

	logsDir := filepath.Join(baseLogsDir, worktreeName)

	// Check if the logs directory exists (means worktree has been implemented)
	if _, err := os.Stat(logsDir); os.IsNotExist(err) {
		// Also check if the worktree itself exists
		worktreesDir, err := core.GetWorktreesDir()
		if err != nil {
			return fmt.Errorf("error getting worktrees dir: %w", err)
		}

		worktreePath := filepath.Join(worktreesDir, worktreeName)
		if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
			return fmt.Errorf("worktree '%s' not found\nRun 'autom8 status' to see available worktrees", worktreeName)
		}

		return fmt.Errorf("no logs found for worktree '%s'\nLogs are created when implementation starts", worktreeName)
	}

	// Set up signal handling for clean exit
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	// Create a done channel to signal shutdown
	done := make(chan struct{})

	// Handle signals in a goroutine
	go func() {
		<-sigChan
		close(done)
	}()

	// Stream logs
	return streamLogs(logsDir, done)
}

// streamLogs continuously streams log files, following new ones as they appear
func streamLogs(logsDir string, done <-chan struct{}) error {
	var currentFile *os.File
	var currentLogFile *logFile
	var lastPos int64

	// Ticker for polling new files and content
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			if currentFile != nil {
				currentFile.Close()
			}
			fmt.Println() // Clean newline on exit
			return nil

		case <-ticker.C:
			// Find all log files and get the latest one
			logFiles, err := findLogFiles(logsDir)
			if err != nil {
				continue // Directory might not exist yet, keep trying
			}

			if len(logFiles) == 0 {
				continue
			}

			// Get the latest log file
			latestLog := logFiles[len(logFiles)-1]

			// Check if we need to switch to a new file
			if currentLogFile == nil || latestLog.path != currentLogFile.path {
				// Close the current file if open
				if currentFile != nil {
					currentFile.Close()
				}

				// Open the new file
				newFile, err := os.Open(latestLog.path)
				if err != nil {
					continue
				}

				currentFile = newFile
				currentLogFile = latestLog
				lastPos = 0

				// Print header when switching files
				fileType := "Implementation"
				if latestLog.isReview {
					fileType = "Review"
				}
				fmt.Printf("\n%s\n", SubtitleStyle.Render(fmt.Sprintf("=== %s iteration %d ===", fileType, latestLog.iteration)))
				fmt.Println()
			}

			// Read new content from current file
			if currentFile != nil {
				// Seek to last position
				currentFile.Seek(lastPos, io.SeekStart)

				// Read new content
				buf := make([]byte, 4096)
				for {
					n, err := currentFile.Read(buf)
					if n > 0 {
						os.Stdout.Write(buf[:n])
						lastPos += int64(n)
					}
					if err != nil || n == 0 {
						break
					}
				}
			}
		}
	}
}

// findLogFiles finds all iteration and review-iteration log files in the directory
func findLogFiles(logsDir string) ([]*logFile, error) {
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		return nil, err
	}

	var files []*logFile

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		info, err := entry.Info()
		if err != nil {
			continue
		}
		modTime := info.ModTime()

		// Check for iteration-N.log
		if matches := iterationPattern.FindStringSubmatch(name); matches != nil {
			iteration, _ := strconv.Atoi(matches[1])
			files = append(files, &logFile{
				path:      filepath.Join(logsDir, name),
				name:      name,
				iteration: iteration,
				isReview:  false,
				modTime:   modTime,
			})
			continue
		}

		// Check for review-iteration-N.log
		if matches := reviewIterationPattern.FindStringSubmatch(name); matches != nil {
			iteration, _ := strconv.Atoi(matches[1])
			files = append(files, &logFile{
				path:      filepath.Join(logsDir, name),
				name:      name,
				iteration: iteration,
				isReview:  true,
				modTime:   modTime,
			})
		}
	}

	// Sort by modification time so the newest file is last.
	sort.Slice(files, func(i, j int) bool {
		if !files[i].modTime.Equal(files[j].modTime) {
			return files[i].modTime.Before(files[j].modTime)
		}
		if files[i].isReview != files[j].isReview {
			return !files[i].isReview && files[j].isReview
		}
		if files[i].iteration != files[j].iteration {
			return files[i].iteration < files[j].iteration
		}
		return files[i].name < files[j].name
	})

	return files, nil
}
