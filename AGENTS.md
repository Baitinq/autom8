# AGENTS.md - autom8 Repository Guide

> **⚠️ MANDATORY: DO NOT EDIT CODE DIRECTLY**
>
> When implementing new features or changes in this repository, you MUST use autom8 itself:
> 1. `autom8 new` - Create a task describing the change
> 2. `autom8 implement` - Let AI agents implement it
> 3. `autom8 converge` - Pick the best implementation
> 4. `autom8 accept` - Merge the winner
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
│       ├── converge.go      # ConvergeCmd + runConverge + doAccept
│       ├── accept.go        # AcceptCmd + runAccept
│       ├── delete.go        # DeleteCmd + runDelete
│       ├── prune.go         # PruneCmd + runPrune
│       ├── inspect.go       # InspectCmd + runInspect
│       ├── describe.go      # DescribeCmd + runDescribe
│       ├── edit.go          # EditCmd + runEdit
│       ├── show.go          # ShowCmd + runShow + pipeToLess
│       ├── chat.go          # ChatCmd + runChat + buildChatSystemPrompt
│       └── agents/          # Embedded agent templates (compiled into binary)
│           ├── implementer.md
│           ├── reviewer.md
│           └── converger.md
├── flake.nix                # Nix flake for dev environment & build
├── flake.lock               # Pinned Nix dependencies
├── go.mod                   # Go module definition
├── README.md                # User documentation
├── TODO                     # Planned work items
└── .autom8/                 # Runtime directory (gitignored except tasks.json)
    ├── tasks.json           # Persisted task definitions (commit this)
    └── worktrees/           # Ephemeral worktree directories (gitignored)
```

## Core Concepts

### Task

The fundamental data structure (defined in `src/core/task.go`) containing:
- **ID** - Unique identifier (user-defined name like `add-login-page`, or auto-generated `task-<unix-nano>`)
- **Prompt** - Implementation instruction
- **VerificationCriteria** - List of success criteria
- **DependsOn** - Optional parent task name
- **CreatedAt** - Timestamp
- **Status** - `pending`, `in-progress`, or `completed`
- **Winner** - Winning worktree name (set by `converge` command)

### Worktrees

Each agent runs in an isolated git worktree at `.autom8/worktrees/{taskID}-{instance}`. This provides:
- Full repository checkout
- Separate branch per implementation
- No conflicts between parallel agents

### Exponential Branching

For dependent tasks, worktrees branch from EACH instance of the parent task:
- Task A with `-n 3` creates 3 worktrees
- Task B (depends on A) with `-n 3` creates 9 worktrees (3 × 3)

## Commands

| Command | Description |
|---------|-------------|
| `autom8 new` | Create a new task (interactive or via flags) |
| `autom8 status` | Display all tasks with status (alias: `list`, `ls`) |
| `autom8 implement -n N` | Run N parallel agents per task |
| `autom8 converge` | Use AI to pick best implementation from multiple worktrees, with a short reasoning summary |
| `autom8 accept <worktree>` | Merge a worktree branch and clean up |
| `autom8 inspect <worktree>` | Open a shell in a worktree directory |
| `autom8 describe <task-id>` | Show detailed task information |
| `autom8 delete <task-id>` | Delete a task |
| `autom8 prune` | Delete all completed tasks |
| `autom8 edit <task-id>` | Edit an existing task |
| `autom8 show <worktree>` | Show diff between main and worktree |
| `autom8 chat <worktree>` | Interactive Claude session in worktree |

### Flag Reference

**`autom8 new`**:
- `-n <name>` - Task name (unique identifier, e.g., `add-login-page`)
- `-p <prompt>` - Task prompt (non-interactive)
- `-c <criterion>` - Verification criterion (repeatable)
- `-d <task-name>` - Dependency task name

**`autom8 edit <task-name>`**:
- `-n <name>` - Rename the task
- `-p <prompt>` - Replace the task prompt
- `-c <criterion>` - Replace verification criteria (repeatable)
- `-d <task-name>` - Change dependency (empty string to remove)

**`autom8 implement`**:
- `-n <count>` - Number of parallel instances per task (default: 1)
- `-m <iterations>` - Maximum iterations per worktree (default: unlimited)

**`autom8 converge`**:
- `-m, --merge` - Auto-merge the winning implementation
  - Output includes the winning worktree and a 1-2 sentence reasoning summary

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
User reviews/merges via standard git
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

- `.autom8/worktrees/` - Recreated on each implement run
- `.direnv/` - Local direnv cache

## Keeping Documentation in Sync

When modifying CLI commands, flags, or user-facing behavior, remember to update:

1. **`.claude/skills/autom8/SKILL.md`** - The Claude Code skill that helps users design tasks interactively. Update this when command syntax, flags, or workflows change.
2. **This file (CLAUDE.md)** - Update the Commands table and Flag Reference sections.
3. **README.md** - User-facing documentation.
