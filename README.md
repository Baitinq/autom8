# autom8

A CLI tool to automate AI agent workflows. Define tasks with prompts and verification criteria, then let AI implement them.

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

**Interactive mode:**
```bash
autom8 feature
```

**Non-interactive mode:**
```bash
autom8 feature -p "Add user authentication" -c "Login endpoint works" -c "Passwords are hashed" -c "JWT tokens are valid"
```

### List tasks

```bash
autom8 implement
```

Output:
```
Found 2 task(s):

1. [pending] Add user authentication
   ID: task-1234567890
   Created: 2026-01-29 15:30:00
   Verification criteria:
     - Login endpoint works
     - Passwords are hashed
     - JWT tokens are valid

2. [pending] Create REST API for products
   ID: task-0987654321
   Created: 2026-01-29 16:00:00
```

## How it works

1. **Define** - Use `autom8 feature` to create a task with a prompt and verification criteria
2. **Store** - Tasks are saved to `~/.autom8/tasks.json`
3. **Implement** - (Coming soon) AI agents will implement tasks and verify against your criteria

## Roadmap

- [ ] AI-powered implementation of tasks
- [ ] Multiple AI backend support (Claude, GPT, local models)
- [ ] Task status management (pending, in_progress, completed)
- [ ] Verification automation
- [ ] Git integration for feature branches

## License

MIT
