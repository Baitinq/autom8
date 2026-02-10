# Agent Memory

This file stores persistent learnings that agents can reference across sessions.

---

## 2026-02-01: Go Build Patterns
[Convention] This project uses cobra for CLI commands. Each command lives in its own file in `src/cmd/` and exports a `XxxCmd` variable that gets registered in `src/main.go` init().

## 2026-02-02: Testing Approach
[Gotcha] The codebase spawns real Claude processes and creates git worktrees. For testing, mock the `exec.Command` calls or use a test repository to avoid side effects.

## 2026-02-03: Styles
[Pattern] Terminal output styling uses lipgloss. Shared styles are defined in `src/cmd/styles.go`. Use these instead of creating new styles to maintain visual consistency.

## 2026-02-04: Task Status Flow
[Architecture] Task status flows: `pending` -> `in-progress` -> `completed`. The `winner` field is only set after auto-converge completes. Never skip states.

## 2026-02-05: Agent Templates
[Convention] Agent prompt templates live in `src/cmd/agents/*.md` and are embedded into the binary at compile time using Go's embed directive. Edit these files to change agent behavior.

## 2026-02-06: Build-time variables
Use -ldflags "-X main.VARNAME=value" pattern for build-time injection. Declare variable in main package, then pass to cmd package via init() to avoid import cycles while keeping the linker-settable variable in the expected location.

## 2026-02-06: Adding CLI commands
New commands follow a consistent pattern: create `src/cmd/<name>.go` with exported `<Name>Cmd` cobra command, register it in `src/main.go` `init()` with `rootCmd.AddCommand()`, and update AGENTS.md commands table plus README.md for documentation.

## 2026-02-06: JSON output flags
When adding --json output to commands, define a dedicated output struct with explicit JSON tags rather than marshaling the internal struct directly. This ensures stable field names (snake_case) and control over which fields are included, preventing accidental exposure of internal fields.

## 2026-02-10: JSON parsing flexibility
When parsing AI/LLM output for embedded JSON, finding the first '{' and last '}' is more robust than looking for specific wrapper patterns (tags, prefixes). This handles varying output formats with less code and fewer edge cases.

## 2026-02-10: Adding new task types
When adding a new task type (like 'review'), follow the pattern: define the type constant in core/task.go, add type-specific fields to the Task struct, separate handling in implement.go by filtering tasks by type, and create a dedicated worker spawner function. The implement command serves as the unified executor for all task types.

## 2026-02-10: Data persistence patterns
In autom8, `.autom8/internal/` is for ephemeral/gitignored data (worktrees, logs, reviews) while `.autom8/` root holds persistent data (tasks.json, memory.md, investigations). Use this distinction when deciding where new artifacts should live.
