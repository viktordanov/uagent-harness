# Subagents: research and plan

Status: planned 2026-09-24 and built the same day (see [As built](#as-built)). Ledger item 12. Codex facts are from openai/codex at rust-v0.156.1 (`C/` is `codex-rs/`); Claude Code facts are from its public documentation.

1. [How Codex does it](#how-codex-does-it)
2. [How Claude Code does it](#how-claude-code-does-it)
3. [What uah has to build on](#what-uah-has-to-build-on)
4. [Plan](#plan)
5. [Open decisions](#open-decisions)
6. [As built](#as-built)

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

## As built

Built 2026-09-24 on unreal-agent-runner v0.1.1.

- **Tools as remote jobs.** `spawn_agent`, `send_input`, `wait`, and `close_agent` are one remote job plan, `uah.agent` v1 (`internal/engine/embedded/agenttool.go`, `agentjobs.go`), next to `uah.mcp_call` in the run's `LocalOperationManager`. Each call runs on its own goroutine, so `wait` never holds up the coordinator: the model gets the runner's "still running" placeholder and keeps working, and the result wakes a new turn. A job that had started before a restart fails instead of running twice, as MCP calls do. The tool names are always resolved, so a session with past agent calls resumes where agents are off.
- **`engine.Subagents`** is the seam: the embedded engine calls `Attach` at each run start with the run's request, its approval function, and its event sink, and offers the tools when `Attach` reports the session may spawn (its depth is below `max_depth`). `internal/agents.Manager` implements it; `app.Setup` builds it only for the embedded engine and binds it to that engine.
- **Children** are `session.Open` sessions on the parent's engine (wrapped so closing a child leaves the shared MCP servers running), with sidecar `source: "subagent"` and `parent`. Their settings are the parent run's provider, model, effort, workspace, and host prompt; the configured defaults, the role, and the call override model and effort in that order, and a role's `developer_instructions` follow the host prompt. `Info.Parent` comes from the sidecar; `session.Interactive` drops subagents from the picker; `session.Tree` puts them under their parent in `uah sessions`.
- **Status.** A child is `running` from the moment a message is sent until its session is idle with every sent message accepted; it is then `completed` with the last run's answer, or `errored`. `close_agent` and closing the parent make it `shutdown`; an unknown ID is `not_found`. `wait` returns every listed child already in a final status, as Codex's v1 does, so waiting again on a finished child returns at once.
- **Limits.** `max_concurrent_threads_per_session` counts open children in the whole tree under the root session; finished children count until closed, as in Codex. Depth comes from the live children and then the sidecars, so a resumed child keeps its depth.
- **Approvals.** A child's session gets `Options.Ask`, which asks through the parent's latest run with `agent <nickname>:` before the justification. The child's own run applies the auto-reviewer first; what it leaves to the user goes to the parent's session (and its PermissionRequest hooks) without a second review. With no one to ask, the child is declined with a reason.
- **Progress.** `engine.AgentUpdated` events go into the parent run's stream; the TUI draws one line per child (`KindAgent`), `/agents` lists them, and `uah run` prints `agent <nickname>: <state>`.
- **Roles** load from `~/.config/uagent/agents` and a trusted workspace's `.uagent/agents` (recursive `*.toml`, later directories win). The subset read: `name`, `description`, `nickname_candidates`, `model`, `model_reasoning_effort`, `developer_instructions`, with Codex's validation; other keys produce a warning notice.
- **Tests.** `internal/agents/agents_test.go` drives one `fakellm` server for parent and children (routes by message content, and replies built from the request for IDs): spawn, wait, and the answer; the coordinator working while a `wait` is pending; a wait timing out; depth 1; `send_input`; the limit and `close_agent`; a child's escalation shown in the parent's session; the picker hiding the child.

Open:

1. **Children across processes.** A child from an earlier process is resumable with `uah resume <id>`, but the parent's `send_input` and `wait` know only the children of the current process (Codex's `resume_agent` is not built).
2. **Updates after the parent's run.** A child's progress reaches the parent's stream only while a parent run is live; after it ends, the TUI line keeps its last state until the next run's updates. `/agents` reads the same lines.
3. **Auto-review transcript.** The auto-reviewer's transcript is per engine, so children's events join the parent's.
4. **`SubagentStop` hook** and ctrl+t details of each child's tool lines are not built.
5. **Interrupting the parent** does not stop its children; closing the session does.
