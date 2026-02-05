# autom8

A CLI tool to automate AI agent workflows. Define tasks with prompts and verification criteria, then let AI implement them in parallel git worktrees.

## Installation

### With Go

```bash
go install github.com/Baitinq/autom8/src@latest
```

### With Nix

```bash
nix develop
# or with direnv
direnv allow
```

### Build from source

```bash
go build -o autom8 ./src
```

## Usage

### Create a task

```bash
# Interactive mode
autom8 new

# Non-interactive mode
autom8 new -p "Add user authentication" -c "Login endpoint works" -c "Passwords are hashed"

# With dependency on another task
autom8 new -p "Add logout button" -d task-1234567890
```

### List tasks

```bash
autom8 list
```

### Implement tasks

```bash
# Implement all pending tasks (one worktree each)
autom8 implement

# Run 3 parallel instances per task
autom8 implement -n 3

# Resume error worktrees across all tasks
autom8 implement --resume

# Resume error worktrees for a specific task
autom8 implement my-task --resume
```

Each task gets its own git worktree in `.autom8/internal/worktrees/`. Tasks with dependencies branch from their dependency's branch.

### Worktree status

Use `autom8 status` or `autom8 describe <task>` to see worktree states:

- **[implementing (N)]** / **[reviewing]** - Agent is actively working
- **[ready]** - Complete, can run `autom8 accept <worktree>` to merge
- **[error]** - Stopped in incomplete state (run `autom8 implement --resume` to restart)
- **[idle]** - No active work

With `-n 3`, you get exponential branching:
- 2 independent tasks = 6 worktrees
- 1 dependent task = 9 worktrees (3 instances per each of 3 parent instances)

### Stream worktree logs

```bash
autom8 logs <worktree>
```

Streams implementation and review logs in real time and switches when new iteration logs are created.

### Wait for completion

```bash
# Wait for a specific task's worktrees to finish
autom8 wait my-task

# Wait for all in-progress tasks
autom8 wait --all
```

Blocks until worktrees reach ready or idle state.

### Auto-converge

When all worktrees for a task finish implementing, auto-converge runs automatically to pick the best implementation using AI. The winner is saved to `tasks.json` and shown in `autom8 describe <task>`.

For tasks with multiple ready worktrees, `autom8 describe` also shows an AI-generated comparison of the implementations.

### Create a draft PR

```bash
# Create a draft PR for a worktree
autom8 pr my-task-1
```

The PR is created as a draft via `gh pr create --draft` using the task prompt and verification criteria.

### Mark tasks complete

```bash
autom8 complete my-task
# or
autom8 done my-task
```

Use this when a task is finished without accepting a worktree (for example, you implemented it manually).

### Cleanup completed tasks

```bash
autom8 prune
```

Prune removes completed tasks and cleans up their worktrees/logs.

## Configuration

Create `.autom8/config.json` to select the implementer and reviewer tools/models. This file is optional; defaults are `claude` for both.

Example:
```json
{
  "implementer": {
    "tool": "claude",
    "model": "sonnet"
  },
  "reviewer": {
    "tool": "codex",
    "model": "gpt-4o"
  }
}
```

Supported tools: `claude`, `codex`, `opencode`.

## How it works

1. **Define** - Use `autom8 new` to create tasks with prompts, verification criteria, and dependencies
2. **Store** - Tasks are saved to `.autom8/tasks.json` (committed to repo)
3. **Implement** - `autom8 implement` creates git worktrees and runs Claude CLI in each
4. **Converge** - When all worktrees finish, auto-converge picks the best implementation
5. **Accept** - `autom8 accept <winner>` merges the winning branch (with AI conflict resolution if needed)

## Data Storage

- `.autom8/tasks.json` - Task definitions (should be committed)
- `.autom8/memory.md` - Persistent learnings from past tasks (should be committed)
- `.autom8/internal/` - Ephemeral data (gitignored)
  - `worktrees/` - Git worktrees for implementations
  - `logs/` - Per-worktree logs (pruned with completed tasks)

## License

MIT
