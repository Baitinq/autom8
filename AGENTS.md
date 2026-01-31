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
│   ├── main.go              # All application logic (single file)
│   └── agents/              # Embedded agent templates (compiled into binary)
│       ├── implementer.md   # Prompt template for implementation agents
│       ├── reviewer.md      # Prompt template for review agents
│       └── converger.md     # Prompt template for convergence agents
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

The fundamental data structure (defined in `src/main.go`) containing:
- **ID** - Unique identifier (`task-<unix-nano>`)
- **Prompt** - Implementation instruction
- **VerificationCriteria** - List of success criteria
- **DependsOn** - Optional parent task ID
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
| `autom8 feature` | Create a new task (interactive or via flags) |
| `autom8 status` | Display all tasks with status (alias: `list`, `ls`) |
| `autom8 implement -n N` | Run N parallel agents per task |
| `autom8 converge` | Use AI to pick best implementation from multiple worktrees |
| `autom8 accept <worktree>` | Merge a worktree branch and clean up |
| `autom8 inspect <worktree>` | Open a shell in a worktree directory |
| `autom8 describe <task-id>` | Show detailed task information |
| `autom8 delete <task-id>` | Delete a task |
| `autom8 prune` | Delete all completed tasks |

### Flag Reference

**`autom8 feature`**:
- `-p <prompt>` - Task prompt (non-interactive)
- `-c <criterion>` - Verification criterion (repeatable)
- `-d <task-id>` - Dependency task ID

**`autom8 implement`**:
- `-n <count>` - Number of parallel instances per task (default: 1)

**`autom8 converge`**:
- `-m, --merge` - Auto-merge the winning implementation

## Code Organization

All logic is in `src/main.go`. Key functions:

- `main()` - CLI argument parsing and command dispatch
- `handleFeature()` - Task creation (interactive & flag-based)
- `handleList()` - Task listing and formatting
- `handleImplement()` - Worktree creation, parallel execution
- `loadTasks()` / `saveTasks()` - JSON persistence to `.autom8/tasks.json`
- `createWorktreeAndRun()` - Creates worktree, spawns Claude CLI

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

1. Add case in `main()` switch statement
2. Create `handleNewCommand()` function
3. Update help text in default case

### Modifying Task structure

1. Update `Task` struct definition
2. Ensure backward compatibility with existing `tasks.json`
3. Update `handleFeature()` for new fields
4. Update `handleList()` display

### Changing Claude invocation

Look for `exec.Command("claude", ...)` in `createWorktreeAndRun()`

## Testing Considerations

- Must run in a git repository (validated at startup)
- Creates real worktrees - use test repos
- Spawns real Claude processes - mock or stub for unit tests
- JSON file operations - ensure cleanup in tests

## Files to Preserve

- `.autom8/tasks.json` - User's task definitions (should be committed)
- `src/agents/*.md` - Prompt templates for AI agents (embedded into binary)

## Files That Are Ephemeral

- `.autom8/worktrees/` - Recreated on each implement run
- `.direnv/` - Local direnv cache
