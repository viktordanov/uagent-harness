<!-- memoria:section id="overview" files="design/harness.md design/tui.md design/implementation.md design/state.md design/sandbox-research.md design/sandbox.md design/subagents.md design/compaction.md design/mcp.md design/usage.md design/images.md configuration.md ledger.md documentation/architecture.md documentation/memoria.md design/shell-mode.md design/streaming.md" -->
# Documentation

<!-- memoria:export id="summary" -->
The configuration reference, design records for the harness, the TUI, state storage, sandboxing, compaction, MCP, subagents, pasted images, and streaming, plus the architecture rules and documentation procedure for uagent-harness.
<!-- /memoria:export -->

Design:

1. [Harness design](design/harness.md): what the runner provides, what the harness adds, the two engines, and the accepted scope.
2. [TUI design](design/tui.md): the framework choice, architecture, screens, keys, and commands.
3. [Implementation spec](design/implementation.md): the packages and files in both repositories, types, milestones, tests, and what was built differently.
4. [State storage](design/state.md): what is stored where, and the plan for a rebuildable SQLite index.
5. [Sandboxing and approvals: research](design/sandbox-research.md): how Codex sandboxes and approves commands, and the options for uah.
6. [Sandboxing and approvals: plan](design/sandbox.md): the decisions (Codex's defaults), how a command runs, the packages, and the phases.
7. [Subagents](design/subagents.md): how Codex and Claude Code run subagents, and the plan for uah.
8. [Compaction](design/compaction.md): how Codex compacts, what the runner supports, and the design with its open decisions.
9. [MCP](design/mcp.md): how Codex runs MCP servers, how their tools run as the runner's remote jobs, OAuth, the validation findings, and the open decisions.
10. [Subscription usage (spike)](design/usage.md): how Codex reads the ChatGPT plan's rate limits, what uah can read with the same login, and the recommended design; the prototype is [internal/usage](../internal/usage/README.md), wired to nothing yet.
11. [Pasting images](design/images.md): how Codex and Claude Code paste images, what the runner and uagent carry, and the design with its open decisions.
12. [Shell mode](design/shell-mode.md): how Codex and Claude Code run a `!` command the user types, and how uah runs it and adds it to the conversation.
13. [Streaming the answer](design/streaming.md): how Codex streams the answer, why the runner needs no change, and how the embedded engine tees each turn request's stream into the TUI.

Reference:

1. [Configuration](configuration.md): every configuration key with its type, default, flag, and merge rule; the home `~/.uah` and its files, the precedence, and complete examples.

Work:

1. [Ledger](ledger.md): the definitive list of work in progress, its scope, lanes, and log.

Maintenance:

1. [Architecture](documentation/architecture.md): the package roles and rules every change follows.
2. [Writing guidance](documentation/writing.md): voice, README shape, and what belongs where.
3. [Section IDs](documentation/sections.md): the IDs that map README sections to source files.
4. [Memoria procedure](documentation/memoria.md): how to review and acknowledge documentation after a change.
<!-- /memoria:section -->
