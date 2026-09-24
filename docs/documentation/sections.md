# Memoria section IDs

Section IDs identify a concern within a README. Keep the ID stable when a heading changes.

| ID | Concern |
| --- | --- |
| `overview` | What the README covers and why it exists |
| `usage` | Commands, inputs, and results |
| `tui` | Keys, commands, and views of the terminal UI |
| `engines` | The embedded and process engines |
| `compaction` | Compaction and the context meter |
| `instructions` | AGENTS.md discovery and the host prompt |
| `hooks` | Hook events, contract, and trust |
| `mcp` | MCP servers: configuration, tools, and results |
| `subagents` | Subagent tools, `[agents]` keys, and role files |
| `seam`, `parity`, `lifecycle`, `tools`, `events`, `resume`, `limits`, `roles`, `extending` | The subagent package's README: its contract with the engine, a child's parity with the root session, a child's lifecycle, the tools, events and hooks, resume, limits, roles, and how to extend it |
| `configuration` | Configuration files and precedence |
| `development` | Building, testing, linting, and CI |

Write the explanation first, then map it to the files that support it:

```markdown
<!-- memoria:section id="hooks" files="internal/hooks/hooks.go internal/hooks/exec.go" -->
## Hooks

Explain the contract.
<!-- /memoria:section -->
```

Paths are literal, relative to the owning README, and must name files that README owns. Do not use globs.
