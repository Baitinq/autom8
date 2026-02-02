# autom8

A CLI tool to automate AI agent workflows. Define tasks with prompts and verification criteria, then let AI implement them in parallel git worktrees.

## Installation

### With Nix (recommended)

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

Each task gets its own git worktree in `.autom8/worktrees/`. Tasks with dependencies branch from their dependency's branch.

### Worktree status

Use `autom8 status` or `autom8 describe <task>` to see worktree states:

- **[implementing (N)]** / **[reviewing]** - Agent is actively working
- **[ready]** - Complete, can run `autom8 accept <worktree>` to merge
- **[error]** - Stopped in incomplete state (run `autom8 implement --resume` to restart)
- **[idle]** - No active work

With `-n 3`, you get exponential branching:
- 2 independent tasks = 6 worktrees
- 1 dependent task = 9 worktrees (3 instances per each of 3 parent instances)

### Converge implementations

```bash
# Converge all tasks with multiple worktrees
autom8 converge

# Converge a specific task
autom8 converge my-task
```

Converge outputs the winning worktree plus a short 1-2 sentence reasoning summary.

### Cleanup completed tasks

```bash
autom8 prune
```

Prune removes completed tasks and cleans up their worktrees/logs.

## How it works

1. **Define** - Use `autom8 new` to create tasks with prompts, verification criteria, and dependencies
2. **Store** - Tasks are saved to `.autom8/tasks.json` (committed to repo)
3. **Implement** - `autom8 implement` creates git worktrees and runs Claude CLI in each

## Data Storage

- `.autom8/tasks.json` - Task definitions (should be committed)
- `.autom8/worktrees/` - Git worktrees for implementations (gitignored)
- `.autom8/logs/` - Per-worktree logs (gitignored; pruned with completed tasks)

## License

MIT
