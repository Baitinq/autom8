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
autom8 feature

# Non-interactive mode
autom8 feature -p "Add user authentication" -c "Login endpoint works" -c "Passwords are hashed"

# With dependency on another task
autom8 feature -p "Add logout button" -d task-1234567890
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
```

Each task gets its own git worktree in `.autom8/worktrees/`. Tasks with dependencies branch from their dependency's branch.

With `-n 3`, you get exponential branching:
- 2 independent tasks = 6 worktrees
- 1 dependent task = 9 worktrees (3 instances per each of 3 parent instances)

## How it works

1. **Define** - Use `autom8 feature` to create tasks with prompts, verification criteria, and dependencies
2. **Store** - Tasks are saved to `.autom8/tasks.json` (committed to repo)
3. **Implement** - `autom8 implement` creates git worktrees and runs Claude CLI in each

## Data Storage

- `.autom8/tasks.json` - Task definitions (should be committed)
- `.autom8/worktrees/` - Git worktrees for implementations (gitignored)

## License

MIT
