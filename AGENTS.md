# AGENTS.md - autom8 Repository Guide

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
│   └── main.go              # All application logic (single file)
├── agents/
│   ├── implementer.md       # Prompt template for implementation agents
│   └── reviewer.md          # Prompt template for review agents
├── flake.nix                # Nix flake for dev environment & build
├── flake.lock               # Pinned Nix dependencies
├── shell.nix                # Alternative Nix shell entry point
├── go.mod                   # Go module definition
├── README.md                # User documentation
├── TODO                     # Planned work items
└── .autom8/                 # Runtime directory (gitignored except tasks.json)
    ├── tasks.json           # Persisted task definitions (commit this)
    └── worktrees/           # Ephemeral worktree directories (gitignored)
```

## Core Concepts

### Task

The fundamental data structure. Defined in `src/main.go`:

```go
type Task struct {
    ID                   string    // Unique: "task-<unix-nano>"
    Prompt               string    // Implementation instruction
    VerificationCriteria []string  // Success criteria
    DependsOn            string    // Optional parent task ID
    CreatedAt            time.Time
    Status               string    // "pending" | "in-progress" | "completed"
}
```

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
| `autom8 feature` | Create a new task (interactive or via flags) |
| `autom8 list` | Display all tasks with status |
| `autom8 implement -n N` | Run N parallel agents per task |

### Flag Reference

**`autom8 feature`**:
- `-p <prompt>` - Task prompt (non-interactive)
- `-c <criterion>` - Verification criterion (repeatable)
- `-d <task-id>` - Dependency task ID

**`autom8 implement`**:
- `-n <count>` - Number of parallel instances per task (default: 1)

## Code Organization

All logic is in `src/main.go` (~520 lines). Key sections:

| Lines | Function | Purpose |
|-------|----------|---------|
| 1-50 | Imports & types | Task struct, global vars |
| 51-150 | `main()` | CLI argument parsing, command dispatch |
| 151-270 | `handleFeature()` | Task creation (interactive & flags) |
| 271-370 | `handleList()` | Task listing and formatting |
| 371-520 | `handleImplement()` | Worktree creation, parallel execution |

### Critical Functions

- `loadTasks()` / `saveTasks()` - JSON persistence to `.autom8/tasks.json`
- `createWorktreeAndRun()` - Creates worktree, spawns Claude CLI
- `handleImplement()` - Orchestrates parallel execution with goroutines

## Dependencies

**Build-time**: Go 1.24+ (defined in `go.mod`)

**Runtime**:
- Git (for worktree operations)
- Claude CLI (`claude` command must be in PATH)

**Go dependencies**: None (stdlib only)

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

1. **Single-file architecture** - All logic in one file for simplicity
2. **Git-native** - Uses worktrees and branches, not custom isolation
3. **Zero dependencies** - Only Go stdlib, minimal attack surface
4. **JSON storage** - Human-readable, version-controllable tasks
5. **Background execution** - Claude processes run detached

## Common Modifications

### Adding a new command

1. Add case in `main()` switch statement (~line 70)
2. Create `handleNewCommand()` function
3. Update help text in default case

### Modifying Task structure

1. Update `Task` struct definition (~line 20)
2. Ensure backward compatibility with existing `tasks.json`
3. Update `handleFeature()` for new fields
4. Update `handleList()` display

### Changing Claude invocation

Look for `exec.Command("claude", ...)` in `createWorktreeAndRun()` (~line 480)

## Testing Considerations

- Must run in a git repository (validated at startup)
- Creates real worktrees - use test repos
- Spawns real Claude processes - mock or stub for unit tests
- JSON file operations - ensure cleanup in tests

## Files to Preserve

- `.autom8/tasks.json` - User's task definitions (should be committed)
- `agents/*.md` - Prompt templates for AI agents

## Files That Are Ephemeral

- `.autom8/worktrees/` - Recreated on each implement run
- `.direnv/` - Local direnv cache
