# AGENTS.md - autom8 Repository Guide

> **⚠️ MANDATORY: DO NOT EDIT CODE DIRECTLY**
>
> When implementing new features or changes in this repository, you MUST use autom8 itself:
> 1. `autom8 new` - Create a task describing the change
> 2. `autom8 implement` - Let AI agents implement it (auto-converge picks winner when done)
> 3. `autom8 accept` - Merge the winner
>
> **NEVER use Edit/Write tools on src/ files directly.** This is a dogfooding requirement.

This document serves as the ground truth for AI agents working on this repository.

## Purpose

**autom8** is a CLI tool that orchestrates AI-driven development workflows. It enables:

- Defining implementation tasks with verification criteria
- Managing task dependencies
- Running multiple Claude AI agents in parallel
- Isolating each agent's work in separate git worktrees

## Repository Structure

```
autom8/
├── src/
│   ├── main.go              # Root command definition, init() wiring, entry point
│   ├── core/
│   │   ├── task.go          # Task struct, LoadTasks, SaveTasks, Truncate
│   │   └── worktree.go      # WorktreeInfo, GetWorktreeInfo, PID tracking
│   └── cmd/
│       ├── styles.go        # Shared terminal output styles
│       ├── new.go           # NewCmd + runFeature
│       ├── status.go        # StatusCmd + runStatus
│       ├── implement.go     # ImplementCmd + runImplement + review loop helpers
│       ├── converge.go      # ConvergeTask (auto-converge logic)
│       ├── accept.go        # AcceptCmd + runAccept
│       ├── delete.go        # DeleteCmd + runDelete
│       ├── prune.go         # PruneCmd + runPrune
│       ├── import.go        # ImportCmd + runImport (AI-based task parsing)
│       ├── inspect.go       # InspectCmd + runInspect
│       ├── investigate.go   # InvestigateCmd + runInvestigate
│       ├── describe.go      # DescribeCmd + runDescribe
│       ├── edit.go          # EditCmd + runEdit
│       ├── show.go          # ShowCmd + runShow + pipeToLess
│       ├── chat.go          # ChatCmd + runChat + buildChatSystemPrompt
│       ├── pr.go            # PrCmd + runPr + buildPrSystemPrompt
│       ├── review.go        # ReviewCmd + runReview + code review convergence
│       └── agents/          # Embedded agent templates (compiled into binary)
│           ├── implementer.md
│           ├── reviewer.md
│           ├── converger.md
│           └── code-reviewer.md
├── flake.nix                # Nix flake for dev environment & build
├── flake.lock               # Pinned Nix dependencies
├── go.mod                   # Go module definition
├── README.md                # User documentation
├── TODO                     # Planned work items
└── .autom8/                 # Runtime directory
    ├── tasks.json           # Persisted task definitions (commit this)
    ├── memory.md            # Persistent learnings from past tasks (commit this)
    └── internal/            # Ephemeral data (gitignored)
        ├── worktrees/       # Git worktree directories
        └── logs/            # Per-worktree logs
```

## Core Concepts

### Task

The fundamental data structure (defined in `src/core/task.go`) containing:
- **ID** - Unique identifier (user-defined name like `add-login-page`, or auto-generated `task-<unix-nano>`)
- **Type** - Either `implementation` (default) or `investigation`
- **Prompt** - Implementation instruction or investigation question
- **VerificationCriteria** - List of success criteria
- **DependsOn** - Optional parent task name
- **CreatedAt** - Timestamp
- **Status** - `pending`, `in-progress`, or `completed`
- **Winner** - Winning worktree name (set by auto-converge)

### Task Types

autom8 supports two types of tasks:

- **Implementation tasks** (default) - For building features, fixing bugs, or making code changes. Created with `autom8 new`. Agents focus on writing and committing code.
- **Investigation tasks** - For understanding, debugging, or researching code. Created with `autom8 investigate`. Agents focus on exploration and write findings to `.autom8/investigations/<task-id>.md`.

### Worktrees

Each agent runs in an isolated git worktree at `.autom8/internal/worktrees/{taskID}-{instance}`. This provides:
- Full repository checkout
- Separate branch per implementation
- No conflicts between parallel agents

### Worktree Status Labels

Worktrees display different status labels in `autom8 status` and `autom8 describe`:

- **[implementing (N)]** - Agent is actively implementing (iteration N)
- **[reviewing]** / **[reviewing (fix N)]** - Agent is in review phase
- **[running]** - Agent is running (fallback when phase unknown)
- **[ready]** - Implementation complete, ready to accept
- **[error]** - Not running but in incomplete state (implementing/reviewing phase or uncommitted changes); restart with `autom8 implement --resume`
- **[idle]** - No active work, no pending changes

Only `[ready]` worktrees show the accept hint (`autom8 accept <worktree>`).

### Exponential Branching

For dependent tasks, worktrees branch from EACH instance of the parent task:
- Task A with `-n 3` creates 3 worktrees
- Task B (depends on A) with `-n 3` creates 9 worktrees (3 × 3)

## Commands

| Command | Description |
|---------|-------------|
| `autom8 new` | Create a new implementation task (interactive or via flags) |
| `autom8 investigate` | Create a new investigation task (interactive or via flags) |
| `autom8 import [file]` | Import tasks from any text format using AI |
| `autom8 status` | Display all tasks with status (alias: `list`, `ls`) |
| `autom8 implement -n N` | Run N parallel agents per task; auto-converge picks winner when all finish |
| `autom8 wait` | Wait for worktrees to complete (blocks until ready/idle) |
| `autom8 pr <worktree>` | Create a draft PR for a worktree |
| `autom8 accept <worktree>` | Merge a worktree branch (with AI conflict resolution) and clean up |
| `autom8 inspect <worktree>` | Open a shell in a worktree directory |
| `autom8 describe <task-id>` | Show detailed task information (with AI comparison when >1 ready worktrees) |
| `autom8 delete <task-id>` | Delete a task |
| `autom8 complete <task-id>` | Mark a task as completed (alias: `done`) |
| `autom8 prune` | Delete all completed tasks and clean up worktrees/logs |
| `autom8 edit <task-id>` | Edit an existing task |
| `autom8 show <worktree>` | Show diff between default branch and worktree |
| `autom8 logs <worktree>` | Stream implementation/review logs for a worktree |
| `autom8 chat <worktree>` | Interactive Claude session in worktree |
| `autom8 review [PR#\|branch]` | Run parallel code reviewers on a PR or branch (defaults to codex) |

### Flag Reference

**`autom8 new`**:
- `-n <name>` - Task name (unique identifier, e.g., `add-login-page`)
- `-p <prompt>` - Task prompt (non-interactive)
- `-c <criterion>` - Verification criterion (repeatable)
- `-d <task-name>` - Dependency task name

**`autom8 investigate`**:
- `-n <name>` - Task name (unique identifier, e.g., `debug-flaky-test`)
- `-p <prompt>` - Investigation question/goal (non-interactive)
- `-c <criterion>` - Success criterion (repeatable)

**`autom8 import [file]`**:
- `[file]` - Optional file to import from (reads from stdin if omitted)
- `-y, --yes` - Skip confirmation prompt (required when reading from stdin)

**`autom8 edit <task-name>`**:
- `-n <name>` - Rename the task
- `-p <prompt>` - Replace the task prompt
- `-c <criterion>` - Replace verification criteria (repeatable)
- `-d <task-name>` - Change dependency (empty string to remove)

**`autom8 implement`**:
- `-n <count>` - Number of parallel instances per task (default: 1)
- `-m <iterations>` - Maximum iterations per worktree (default: unlimited)
- `--add` - Add more implementations to an existing task (allows in-progress tasks)
- `--resume` - Restart workers for worktrees in error state (ignores `-n`)

When all worktrees for a task finish, auto-converge runs automatically to pick the best implementation. The winner is saved to `tasks.json`.

**`autom8 wait`**:
- `autom8 wait <task-name>` - Wait for a specific task's worktrees to complete
- `autom8 wait --all` - Wait for all in-progress tasks

**`autom8 pr`**:
- `autom8 pr <worktree>` - Create a draft PR for the specified worktree

**`autom8 review`**:
- `[PR#|branch]` - Optional PR number or branch to review (defaults to current branch)
- `-n <count>` - Number of parallel reviewers (default: 1)

The reviewer tool defaults to `codex` (configurable via `.autom8/config.json`).

## Code Organization

Code is organized into three packages:

### `main` (src/main.go)
- Defines `rootCmd` cobra command
- `init()` registers all subcommands from the `cmd` package
- `main()` executes the root command

### `core` (src/core/)
- `task.go`: `Task` struct, `LoadTasks()`, `SaveTasks()`, `GetGitRoot()`, `GetAutom8Dir()`, `EnsureAutom8Dir()`, `Truncate()`
- `worktree.go`: `WorktreeInfo` struct, `GetWorktreeInfo()`, `LoadPids()`, `SavePids()`, `IsProcessRunning()`

### `cmd` (src/cmd/)
- Each command has its own file exporting a `XxxCmd` variable
- `styles.go`: Shared lipgloss styles for terminal output
- `agents/`: Embedded agent templates (implementer.md, reviewer.md, converger.md)

## Dependencies

**Build-time**: Go 1.24+ (defined in `go.mod`)

**Runtime**:
- Git (for worktree operations)
- Claude CLI (`claude` command must be in PATH)

**Go dependencies**:
- `github.com/spf13/cobra` - CLI framework
- `github.com/charmbracelet/huh` - Interactive forms
- `github.com/charmbracelet/lipgloss` - Terminal styling

## Build & Run

```bash
# With Nix
nix develop   # or: direnv allow
go build -o autom8 ./src

# Without Nix
go build -o autom8 ./src
```

## Data Flow

```
User defines task
       ↓
tasks.json (persisted)
       ↓
autom8 implement -n N
       ↓
┌──────────────────────────────────────┐
│  For each pending task:              │
│    For each instance 1..N:           │
│      1. Create git worktree          │
│      2. Create branch from base      │
│      3. Spawn: claude -p "..." &     │
└──────────────────────────────────────┘
       ↓
Multiple branches with implementations
       ↓
Auto-converge picks winner (when all worktrees finish)
       ↓
autom8 accept <winner> to merge
```

## Branch Naming

- Independent tasks: `autom8/{taskID}-{instance}`
- Dependent tasks: `autom8/{parentID}-{parentInstance}-{instance}`

## Key Design Decisions

1. **Multi-file architecture** - Organized by package (core, cmd) for maintainability
2. **Git-native** - Uses worktrees and branches, not custom isolation
3. **Minimal dependencies** - Only essential libraries (cobra, huh, lipgloss)
4. **JSON storage** - Human-readable, version-controllable tasks
5. **Iterative execution** - Claude processes run in loops until "TASK COMPLETE"

## Common Modifications

### Adding a new command

1. Create `src/cmd/newcommand.go` with `NewCommandCmd` variable
2. Add `rootCmd.AddCommand(cmd.NewCommandCmd)` in `src/main.go` `init()`

### Modifying Task structure

1. Update `Task` struct in `src/core/task.go`
2. Ensure backward compatibility with existing `tasks.json`
3. Update relevant commands in `src/cmd/`

### Changing Claude invocation

Look for `exec.Command("claude", ...)` in `src/cmd/implement.go` or `src/cmd/chat.go`

## Testing Considerations

- Must run in a git repository (validated at startup)
- Creates real worktrees - use test repos
- Spawns real Claude processes - mock or stub for unit tests
- JSON file operations - ensure cleanup in tests

## Files to Preserve

- `.autom8/tasks.json` - User's task definitions (should be committed)
- `src/cmd/agents/*.md` - Prompt templates for AI agents (embedded into binary)
- `.claude/skills/autom8/SKILL.md` - Claude Code skill for interactive task design

## Files That Are Ephemeral

- `.autom8/internal/` - Contains worktrees and logs, recreated on each implement run
- `.direnv/` - Local direnv cache

## Keeping Documentation in Sync

When modifying CLI commands, flags, or user-facing behavior, remember to update:

1. **`.claude/skills/autom8/SKILL.md`** - The Claude Code skill that helps users design tasks interactively. Update this when command syntax, flags, or workflows change.
2. **This file (CLAUDE.md)** - Update the Commands table and Flag Reference sections.
3. **README.md** - User-facing documentation.
