<!-- memoria:section id="overview" files="design/harness.md design/tui.md design/implementation.md design/state.md design/sandbox-research.md design/sandbox.md design/subagents.md design/compaction.md design/mcp.md design/usage.md design/images.md configuration.md ledger.md documentation/architecture.md documentation/memoria.md design/shell-mode.md design/streaming.md design/markdown.md design/rewind.md design/selection.md design/codex-auth.md design/hosting.md design/editor.md design/system-prompt.md design/web-search.md design/review.md design/prompt-history.md design/tool-calls.md design/keys.md" -->
# Documentation

<!-- memoria:export id="summary" -->
The configuration reference, design records for the harness, the TUI, state storage, sandboxing, compaction, MCP, subagents, pasted images, streaming, Markdown rendering, going back to an earlier message, selecting text with the mouse, editing the prompt in an editor, the system prompt, web search, `/diff` and `/review`, prompt history and the composer's height, how tool calls read in the transcript, keeping the ChatGPT login fresh, and running uah as a terminal host backend, plus the architecture rules and documentation procedure for uah.
<!-- /memoria:export -->

Design:

1. [Harness design](design/harness.md): what the runner provides, what the harness adds, the two engines, and the accepted scope.
2. [TUI design](design/tui.md): the framework choice, architecture, screens, keys, and commands.
3. [Implementation spec](design/implementation.md): the packages and files in both repositories, types, milestones, tests, and what was built differently.
4. [State storage](design/state.md): what is stored where, and the plan for a rebuildable SQLite index.
5. [Sandboxing and approvals: research](design/sandbox-research.md): how Codex sandboxes and approves commands, and the options for uah.
6. [Sandboxing and approvals: plan](design/sandbox.md): the decisions (Codex's defaults), how a command runs, the packages, and the phases.
7. [Subagents](design/subagents.md): how Codex and Claude Code run subagents, and the plan for uah.
8. [Compaction](design/compaction.md): how Codex compacts, locally and remotely, what the runner supports, the design, the offline evaluation over recorded sessions and its numbers, the state ledger, elision, the kept calls and the summary prompt, remote compaction with its probes, and the open decisions.
9. [MCP](design/mcp.md): how Codex runs MCP servers, how their tools run as the runner's remote jobs, OAuth, the validation findings, and the open decisions.
10. [Subscription usage (spike)](design/usage.md): how Codex reads the ChatGPT plan's rate limits, what uah can read with the same login, and the recommended design; the prototype is [internal/usage](../internal/usage/README.md), wired to nothing yet.
11. [Pasting images](design/images.md): how Codex and Claude Code paste images, what the runner and uagent carry, and the design with its open decisions.
12. [Shell mode](design/shell-mode.md): how Codex and Claude Code run a `!` command the user types, and how uah runs it and adds it to the conversation.
13. [Streaming the answer](design/streaming.md): how Codex streams the answer, why the runner needs no change, and how the embedded engine tees each turn request's stream into the TUI.
14. [Markdown rendering](design/markdown.md): how Codex draws and streams Markdown, the parser, incremental rendering by blocks, tables that fit the width, cached highlighting, the look chosen for code, tables, quotes, and headings, and the benchmarks that gate it.
15. [Going back to an earlier message](design/rewind.md): Codex's backtrack and Claude Code's rewind, and how uah cuts the context at an earlier message while the session file keeps the old branch.
16. [Selecting and copying text](design/selection.md): what Codex and other terminal programs do with the mouse, and how uah selects transcript text, keeps it on its text while the transcript moves, and copies it on release.
17. [Keeping the ChatGPT login fresh](design/codex-auth.md): when and how Codex refreshes its token in `auth.json`, and how uah does the same with Codex writing the same file.
18. [Integration with the terminal host](design/hosting.md): what the terminal host needs from a harness, and the changes that let it run uah inside the mechanisms it keeps for every harness.
19. [Editing the prompt in an editor](design/editor.md): what Claude Code's and Codex's ctrl+g do, and how uah runs `$VISUAL` or `$EDITOR` on a draft file the sandbox cannot reach, with the terminal released, and keeps its images.
20. [The system prompt](design/system-prompt.md): Codex's prompt for gpt-6.1-sol with the nine changes uah needs, each with its reason, and Codex's `<environment_context>` at the end of the system message.
21. [Web search](design/web-search.md): how Codex offers the hosted `web_search` tool, what the runner sends and drops, how uah offers it, shows each search, and puts the dropped searches back into later requests by insertion, the probes on openai-codex, and the limits.
22. [`/diff` and `/review`](design/review.md): how Codex collects and shows the git diff and runs a code review in a separate thread, and how uah shows the diff and runs a read-only reviewer whose findings reach the agent.
23. [Prompt history and a taller composer](design/prompt-history.md): how Codex stores prompts in `history.jsonl`, recalls them with ↑ and ↓, and searches them with ctrl+r, what Claude Code does, and how uah does the same with Codex's file format plus each prompt's workspace, shows each folder only its own prompts as Claude Code does, resolves the key conflicts, and grows the composer to half the window.
24. [Tool calls in the transcript](design/tool-calls.md): the owner's pick from the gallery (R6 with spacing b), the port of Codex's command classifier, where the error lines and MCP results come from, and how an approval finds its call.
25. [Send keys](design/keys.md): how Codex and Claude Code bind send-now and queue, what the terminal reports about ctrl+enter and shift+enter (tmux measured), and why uah binds enter to send before the next model request and tab to queue in every terminal, as Codex does.
26. [Agent tuning](design/agent-tuning.md): every agent-benchmark measurement of uah against Codex, the experiments (freeform `apply_patch`, async prompts, wake policies), what was decided, and the release-note summary.

Reference:

1. [Configuration](configuration.md): every configuration key with its type, default, flag, and merge rule; the home `~/.uah` and its files, the configuration layers, the precedence, and complete examples.

Work:

1. [Ledger](ledger.md): the definitive list of work in progress, its scope, lanes, and log.

Maintenance:

1. [Architecture](documentation/architecture.md): the package roles and rules every change follows.
2. [Writing guidance](documentation/writing.md): voice, README shape, and what belongs where.
3. [Section IDs](documentation/sections.md): the IDs that map README sections to source files.
4. [Memoria procedure](documentation/memoria.md): which README covers a file, and how to review and acknowledge documentation after a change.
<!-- /memoria:section -->
