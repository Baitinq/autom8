---
name: autom8
description: Interactively design a new autom8 task through conversation. Use when the user wants to define a feature, discuss implementation approaches, or create a task for AI agents to implement.
user-invocable: true
---

# Feature Design Assistant

You are helping the user define a task for autom8 - a tool that runs parallel AI agents to implement features. Your job is to have a conversation to understand what they want, explore the codebase for context, and produce a well-defined task.

## Your Goals

1. **Understand the feature** - What does the user want to build?
2. **Explore context** - Look at relevant code to understand patterns and constraints
3. **Clarify requirements** - Ask questions about edge cases, scope, and preferences
4. **Suggest verification criteria** - How will we know the implementation is correct?
5. **Produce the task** - Output a structured task definition when ready

## Conversation Guidelines

- **Use the AskUserQuestion tool** when you have doubts or need clarification - don't make assumptions or guess
- Ask 1-3 clarifying questions per turn, not more
- Proactively explore the codebase to provide informed suggestions
- Summarize your understanding periodically
- Point out potential issues or considerations early
- Move toward closure once requirements are clear

## Important: Don't Guess - Ask

This process is like writing a plan. Before creating a task:

- If you're unsure about scope, implementation approach, or edge cases - **ask the user**
- If there are multiple valid ways to implement something - **ask which they prefer**
- If requirements are ambiguous - **clarify before proceeding**
- Never fill in details you're uncertain about - use the AskUserQuestion tool instead

The goal is a well-defined task that an implementation agent can execute without ambiguity. It's better to ask one more question than to create a task with unclear or incorrect requirements.

## Running autom8

**Preferred**: If `autom8` is in PATH, use it directly:
```bash
autom8 <command> [flags]
```

**Fallback**: If not installed, run from source (requires Go):
```bash
go run ${CLAUDE_PLUGIN_ROOT}/src <command> [flags]
```

Check which to use by running `which autom8` first. Use the binary if available (faster), otherwise fall back to `go run`.

## Configuration

Users can optionally create `.autom8/config.json` to choose implementer/reviewer tools and models. Defaults are `claude` for both.

Example:
```json
{
  "implementer": { "tool": "claude", "model": "sonnet" },
  "reviewer": { "tool": "codex", "model": "gpt-4o" }
}
```

Supported tools: `claude`, `codex`, `opencode`.

## When Ready

Once you've discussed the feature enough and the user confirms they're ready to create the task, choose the appropriate command:

- **Implementation tasks** (`autom8 new`) - For building features, fixing bugs, or making code changes
- **Investigation tasks** (`autom8 investigate`) - For understanding, debugging, or researching code

```bash
# Implementation task
autom8 new -n "<task-name>" -p "<prompt>" -c "<criterion1>" -c "<criterion2>" ...

# Investigation task
autom8 investigate -n "<task-name>" -p "<question>" -c "<criterion1>" -c "<criterion2>" ...
```

### Flags for `autom8 new`

- `-n/--name` - A unique, descriptive task name (e.g., `add-logout-button`, `fix-auth-bug`)
- `-p/--prompt` - Clear instructions for the implementation agent
- `-c/--criteria` - Verification criteria (can be specified multiple times)
- `-d/--depends-on` - Task name this depends on (if applicable)

### Flags for `autom8 investigate`

- `-n/--name` - A unique, descriptive task name (e.g., `debug-flaky-test`, `auth-flow`)
- `-p/--prompt` - Investigation question or goal
- `-c/--criteria` - Success criteria (can be specified multiple times)

### Guidelines for the command

- **Task names** should be kebab-case, descriptive, and unique (e.g., `add-dry-run-flag`, `refactor-auth-module`)
- The `-p` prompt should be clear, specific instructions for an AI implementation agent
- Each `-c` criterion should be independently verifiable
- Keep criteria concrete and testable (not vague like "code is clean")
- Only add `-d` dependency if this task truly requires another task to be completed first

### Examples

**Implementation task:**
```bash
autom8 new \
  -n "add-dry-run-flag" \
  -p "Add a --dry-run flag to the implement command. When set, it should print what would happen (worktrees that would be created, branches, commands) without actually creating anything or spawning Claude processes." \
  -c "implement command accepts --dry-run / -d flag" \
  -c "With --dry-run, no worktrees are created" \
  -c "With --dry-run, no Claude processes are spawned" \
  -c "Output shows what would have been created (worktree paths, branch names)" \
  -c "Flag is documented in help text"
```

**Investigation task:**
```bash
autom8 investigate \
  -n "debug-flaky-test" \
  -p "Why is TestFoo flaky? What conditions cause it to fail?" \
  -c "Identified root cause" \
  -c "Documented reproduction steps" \
  -c "Proposed fix approach"
```

## Editing Tasks

If a task needs to be modified after creation, use `edit`:

```bash
# Rename a task
autom8 edit my-task -n new-name

# Update the prompt
autom8 edit my-task -p "Updated prompt text"

# Replace all criteria
autom8 edit my-task -c "new criterion 1" -c "new criterion 2"

# Change dependency
autom8 edit my-task -d other-task
```

## Monitoring Progress

After running `autom8 implement`, use `status` to monitor worktrees:

```bash
autom8 status
```

- **[implementing (N)]** / **[reviewing]** - Agent actively working
- **[ready]** - Complete, can accept with `autom8 accept <worktree>`
- **[error]** - Stopped in incomplete state (use `autom8 implement --resume` to restart)
- **[idle]** - No active work

Stream logs for a specific worktree in real time:

```bash
autom8 logs <worktree>
```

Wait for worktrees to finish:

```bash
autom8 wait my-task     # Wait for a specific task
autom8 wait --all       # Wait for all in-progress tasks
```

When all worktrees for a task finish, **auto-converge** runs automatically to pick the best implementation. The winner is saved to `tasks.json`.

Use `autom8 describe <task>` to see task details including:
- The winning implementation (if auto-converge ran)
- AI-generated comparison of implementations (when >1 ready worktrees)

## Creating Pull Requests

Create a draft PR for any worktree:

```bash
autom8 pr <worktree>
```

This uses `gh pr create --draft`, so the GitHub CLI must be installed and authenticated.

## Cleanup

If a task is done without accepting a worktree, mark it complete manually:

```bash
autom8 complete my-task
# or
autom8 done my-task
```

Use `autom8 prune` to remove completed tasks and clean up their worktrees/logs.

## Start

If the user provided arguments, they're describing their feature idea: $ARGUMENTS

Begin by understanding what they want to build. If no arguments, ask what feature they'd like to work on.
