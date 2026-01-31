# Agent Memory

This file stores persistent learnings that agents can reference across sessions. Keep entries concise and actionable.

**Maximum size: 100 lines.** When approaching this limit, agents should consolidate or remove outdated entries.

## Format

Each entry follows this format:
```
### [Category] Brief title
Added: YYYY-MM-DD
Context: One line explaining when this is relevant
Learning: The actual insight or pattern (keep to 1-3 lines)
```

Categories: `[Pattern]`, `[Gotcha]`, `[Convention]`, `[Architecture]`, `[Tool]`

---

## Entries

<!-- Add new entries below this line. Oldest entries should be removed first when consolidating. -->

### [Architecture] Single-file design
Added: 2026-02-01
Context: When modifying core logic
Learning: All application logic lives in src/main.go. This is intentional for simplicity - don't refactor into multiple files.

### [Convention] Agent templates are embedded
Added: 2026-02-01
Context: When updating agent prompts
Learning: Agent templates in src/agents/*.md are embedded into the binary at build time via Go embed. Changes require rebuild.
