package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	autom8Dir = ".autom8"
	tasksFile = "tasks.json"
)

type Task struct {
	ID                   string    `json:"id"`
	Prompt               string    `json:"prompt"`
	VerificationCriteria []string  `json:"verification_criteria"`
	CreatedAt            time.Time `json:"created_at"`
	Status               string    `json:"status"`
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "feature":
		runFeature()
	case "implement":
		runImplement()
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("autom8 - Automate AI agent workflows")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  autom8 <command> [options]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  feature    Create a new task/prompt")
	fmt.Println("             -p <prompt>    Task prompt (non-interactive)")
	fmt.Println("             -c <criteria>  Verification criteria (repeatable)")
	fmt.Println("  implement  List all saved tasks")
	fmt.Println("  help       Show this help message")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  autom8 feature")
	fmt.Println("  autom8 feature -p \"Add login page\" -c \"Has email field\" -c \"Has password field\"")
}

func getAutom8Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, autom8Dir), nil
}

func ensureAutom8Dir() (string, error) {
	dir, err := getAutom8Dir()
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	return dir, nil
}

func loadTasks() ([]Task, error) {
	dir, err := getAutom8Dir()
	if err != nil {
		return nil, err
	}

	tasksPath := filepath.Join(dir, tasksFile)

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

func saveTasks(tasks []Task) error {
	dir, err := ensureAutom8Dir()
	if err != nil {
		return err
	}

	tasksPath := filepath.Join(dir, tasksFile)

	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(tasksPath, data, 0644)
}

func readMultilineInput(reader *bufio.Reader) string {
	var lines []string

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}

		line = strings.TrimRight(line, "\r\n")

		if line == "" {
			break
		}
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

type arrayFlags []string

func (a *arrayFlags) String() string {
	return strings.Join(*a, ", ")
}

func (a *arrayFlags) Set(value string) error {
	*a = append(*a, value)
	return nil
}

func runFeature() {
	featureCmd := flag.NewFlagSet("feature", flag.ExitOnError)
	promptFlag := featureCmd.String("p", "", "Task prompt (non-interactive mode)")
	var criteriaFlags arrayFlags
	featureCmd.Var(&criteriaFlags, "c", "Verification criteria (can be specified multiple times)")

	featureCmd.Parse(os.Args[2:])

	var prompt string
	var criteria []string

	if *promptFlag != "" {
		// Non-interactive mode
		prompt = *promptFlag
		criteria = criteriaFlags
	} else {
		// Interactive mode
		reader := bufio.NewReader(os.Stdin)

		fmt.Println("Enter your task/prompt (press Enter to finish):")
		fmt.Println()

		prompt = readMultilineInput(reader)

		if strings.TrimSpace(prompt) == "" {
			fmt.Println("No prompt entered. Aborting.")
			return
		}

		fmt.Println()
		fmt.Println("Enter verification criteria (one per line, empty line to finish):")
		fmt.Println()

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				break
			}

			line = strings.TrimRight(line, "\r\n")

			if line == "" {
				break
			}
			criteria = append(criteria, line)
		}
	}

	if strings.TrimSpace(prompt) == "" {
		fmt.Println("No prompt provided. Aborting.")
		return
	}

	tasks, err := loadTasks()
	if err != nil {
		fmt.Printf("Error loading tasks: %v\n", err)
		os.Exit(1)
	}

	task := Task{
		ID:                   fmt.Sprintf("task-%d", time.Now().UnixNano()),
		Prompt:               prompt,
		VerificationCriteria: criteria,
		CreatedAt:            time.Now(),
		Status:               "pending",
	}

	tasks = append(tasks, task)

	if err := saveTasks(tasks); err != nil {
		fmt.Printf("Error saving task: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Printf("Task saved with ID: %s\n", task.ID)
}

func runImplement() {
	tasks, err := loadTasks()
	if err != nil {
		fmt.Printf("Error loading tasks: %v\n", err)
		os.Exit(1)
	}

	if len(tasks) == 0 {
		fmt.Println("No tasks found. Use 'autom8 feature' to create one.")
		return
	}

	fmt.Printf("Found %d task(s):\n\n", len(tasks))

	for i, task := range tasks {
		fmt.Printf("%d. [%s] %s\n", i+1, task.Status, truncate(task.Prompt, 60))
		fmt.Printf("   ID: %s\n", task.ID)
		fmt.Printf("   Created: %s\n", task.CreatedAt.Format("2006-01-02 15:04:05"))
		if len(task.VerificationCriteria) > 0 {
			fmt.Println("   Verification criteria:")
			for _, c := range task.VerificationCriteria {
				fmt.Printf("     - %s\n", c)
			}
		}
		fmt.Println()
	}
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
