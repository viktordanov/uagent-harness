# Subagents: research and plan

Status: planned 2026-09-24, not built. Ledger item 12. Codex facts are from openai/codex at rust-v0.156.1 (`C/` is `codex-rs/`); Claude Code facts are from its public documentation.

1. [How Codex does it](#how-codex-does-it)
2. [How Claude Code does it](#how-claude-code-does-it)
3. [What uah has to build on](#what-uah-has-to-build-on)
4. [Plan](#plan)
5. [Open decisions](#open-decisions)

## How Codex does it

- **Tools.** v1: `spawn_agent`, `send_input`, `wait`, `close_agent`, `resume_agent` (`C/core/src/tools/handlers/multi_agents/`). v2 (behind `features.multi_agent_v2`): `spawn_agent` with a required `task_name` and `message`, `send_message`, `followup_task`, `wait`, `list_agents`, `interrupt_agent` (`multi_agents_v2/`). `spawn_agent` returns the agent's ID and a nickname; the parent keeps working and calls `wait` only when it needs the result.
- **Children are threads.** A spawned agent is another conversation thread of the same session manager, inheriting the parent's model, sandbox, and approval policy unless the call or the role overrides model and reasoning effort.
- **Configuration.** `[agents]` in `config.toml` (`C/config/src/config_toml.rs:695`): `enabled`, `max_concurrent_threads_per_session` (alias `max_threads`), `max_depth` (v1 nesting), `default_subagent_model`, `default_subagent_reasoning_effort`.
- **Roles.** `C/agent-roles/`: role files with `name`, `description`, `nickname_candidates`, and any config keys as a layer over the parent's config (model, effort, instructions). The role list and descriptions go into the `spawn_agent` tool description.
- **The prompt restrains spawning.** The tool description says: do not spawn unless the user or AGENTS.md/skills ask for delegation; research requests alone do not count; keep critical-path work local; give subagents disjoint write sets; call `wait` sparingly (`multi_agents_spec.rs:674`).

## How Claude Code does it

- **One tool.** `Task` (now "Agent") starts a subagent with a prompt and a `subagent_type`; the subagent runs in its own context window and returns one final report as the tool result. Several can run in parallel; subagents cannot start their own subagents.
- **Definitions.** Markdown files with YAML front matter in `~/.claude/agents/` and `.claude/agents/`: `name`, `description` (when to use it), optional `tools` (an allow-list) and `model`; the body is the system prompt.
- **Background runs.** A subagent can run in the background, and the parent is notified when it finishes.

## What uah has to build on

- **Sessions and engines.** A uah session owns a runner session; the embedded engine can run several coordinators in one process, each with its own session ID, inbox, and store (the runner's store is per session ID).
- **Asynchronous tools.** A spawned agent must not block the parent's coordinator. MCP (ledger item 5) introduces the mechanism for tools that run outside the coordinator; subagent tools use the same one.
- **Approvals and the sandbox** (items 2 and 3) apply to children unchanged; a child's approval prompt appears in the parent's TUI, labelled with the child's nickname.
- **Hooks** (M6) fire for children too; `SubagentStop` is added then.

## Plan

Follow Codex's v1 tool set, which covers Claude Code's single tool as its simplest use:

| Tool | Does |
| --- | --- |
| `spawn_agent(message, agent_type?, model?, reasoning_effort?)` | Starts a child session in the parent's workspace with the parent's sandbox and approvals; returns `{id, nickname}` at once |
| `send_input(id, message)` | Sends a follow-up message to a running or idle child (live on the embedded engine) |
| `wait(ids, timeout?)` | Returns when any listed child finishes or the timeout passes, with each finished child's final answer |
| `close_agent(id)` | Stops a child |

- **Children** are ordinary uah sessions (`session.Open` with a new ID and a sidecar `source: "subagent"` plus the parent's ID), so they are resumable, listed under the parent in `uah sessions`, and hidden from the resume picker.
- **Configuration** mirrors Codex: `[agents] enabled, max_concurrent_threads_per_session (default 4), max_depth (default 1: children cannot spawn), default_subagent_model, default_subagent_reasoning_effort`.
- **Roles** use Codex's role files, loaded from `~/.config/uagent/agents/*.toml` and trusted `.uagent/agents/*.toml`. The spawn tool description lists them, followed by Codex's restraint guidance, adapted.
- **TUI.** A child's progress is one line under the spawning tool call ("• agent reviewer: running 0:42"); `/agents` lists children; ctrl+t details show each child's tool lines.
- **Tests.** `fakellm` scripts for parent and child (one server, requests routed by the session ID in the prompt-cache key): spawn → wait → answer, a child's approval surfacing in the parent, the concurrency limit, depth 1.

Build it after items 2–5 merge, because it depends on asynchronous tools and approvals.

## Open decisions

Defaults taken; change them here before building.

1. **Workspace sharing.** Children share the parent's workspace (Codex v1). Codex's "forked workspace" guidance for code-edit subtasks is not built; git worktrees per child could come later.
2. **Claude Code's Markdown agent definitions** are not read. Codex role files only, for one format.
3. **v2 tools** (`task_name`, `followup_task`, `list_agents`, `interrupt_agent`) are left out until Codex makes v2 the default.
