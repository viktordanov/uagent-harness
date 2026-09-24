# Subagents: research and plan

Status: planned 2026-09-24, built the same day, then validated and hardened (see [As built](#as-built) and [Validation](#validation)). Ledger item 12. Codex facts are from openai/codex at rust-v0.156.1 (`C/` is `codex-rs/`); Claude Code facts are from its public documentation.

1. [How Codex does it](#how-codex-does-it)
2. [How Claude Code does it](#how-claude-code-does-it)
3. [What uah has to build on](#what-uah-has-to-build-on)
4. [Plan](#plan)
5. [Open decisions](#open-decisions)
6. [As built](#as-built)
7. [Validation](#validation)

## How Codex does it

- **Tools.** v1: `spawn_agent`, `send_input`, `wait_agent`, `close_agent`, `resume_agent` (`C/core/src/tools/handlers/multi_agents/`), taking `target` (`targets` for `wait_agent`, `id` for `resume_agent`) and returning `{agent_id, nickname}`, `{submission_id}`, `{status, timed_out}`, `{previous_status}`, and `{status}`. A status is `AgentStatus` in JSON: `"pending_init"`, `"running"`, `"interrupted"`, `"shutdown"`, `"not_found"`, `{"completed": message}`, or `{"errored": message}`; `interrupted` is not final (`C/core/src/agent/status.rs`). v2 (behind `features.multi_agent_v2`): `spawn_agent` with a required `task_name` and `message`, `send_message`, `followup_task`, `wait`, `list_agents`, `interrupt_agent` (`multi_agents_v2/`). `spawn_agent` returns the agent's ID and a nickname; the parent keeps working and calls `wait` only when it needs the result.
- **Children are threads.** A spawned agent is another conversation thread of the same session manager, inheriting the parent's model, sandbox, and approval policy unless the call or the role overrides model and reasoning effort.
- **Configuration.** `[agents]` in `config.toml` (`C/config/src/config_toml.rs:695`): `enabled`, `max_concurrent_threads_per_session` (alias `max_threads`), `max_depth` (v1 nesting), `default_subagent_model`, `default_subagent_reasoning_effort`.
- **Roles.** `C/agent-roles/`: role files with `name`, `description`, `nickname_candidates`, and any config keys as a layer over the parent's config (model, effort, instructions). The role list and descriptions go into the `spawn_agent` tool description.
- **Interrupts and completion.** Interrupting a thread (`Op::Interrupt`) aborts only that thread's turn (`abort_all_tasks`); its children keep running, and `send_input` with `interrupt: true` interrupts one child. When a child reaches a final status, a watcher injects a `<subagent_notification>` fragment into the parent's history without starting a turn (`C/core/src/agent/control.rs`, `maybe_start_completion_watcher`).
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

Built 2026-09-24 on unreal-agent-runner v0.1.1, and hardened the same day (see [Validation](#validation)). The code and its lifecycle are described in [internal/agents/README.md](../../internal/agents/README.md).

- **The seam.** `engine.Subagents` has four methods: `Attach` records a parent's run and returns the tools to offer it, `ToolNames` lists every name a call may use, `Call` runs one call, and `Interrupt` stops a parent's children. The embedded engine offers what `Attach` returns, resolves `ToolNames` even when nothing is offered (so a session with past calls resumes), and runs each call as a remote job, `uah.agent` v1, next to `uah.mcp_call` in the run's `LocalOperationManager`; it knows no tool name, schema, or result. A call runs on its own goroutine, so `wait_agent` never holds up the coordinator. A job that had started before a restart fails instead of running twice.
- **Tools** are Codex's v1 set with its names, parameters, results, and status encoding. Left out: `items` (structured input) and `fork_context` (a child starting from the parent's history), which the runner cannot do without changes. `wait` (uah's first name for `wait_agent`) still resolves but is not offered.
- **Children are the root agent** in every way except what makes them children. A child opens with `session.Open` on the parent's engine (wrapped so closing a child leaves the shared engine running) and the session options `app.Setup` returned for the root session, so it gets the same instructions and skills, sandbox, rules, approvals and auto-review, hooks, MCP servers, compaction, context meter, output limits, index, and sidecar. Its settings are the parent run's (`Settings.WithRequest` inverts the run's request) with its service tier. The only differences: its session ID and sidecar (`source: "subagent"`, `parent`), the spawn tools gated by `max_depth`, approvals through the parent, the model, effort, and instructions a role, the call, or `[agents]` defaults set, and a hook runner of its own. `sessions/<id>.agent.json` keeps the nickname, role, and the spawn call's model and effort for `resume_agent`.
- **Status.** A child is `running` from the moment a message is sent until its session is idle with every sent message accepted; it is then `completed` with the last run's answer, `interrupted`, or `errored`. `close_agent` and closing the parent make it `shutdown`; an unknown ID is `not_found`; a resumed child is `pending_init` until it gets a message. `wait_agent` returns every listed child already in a final status; `interrupted` is final here (see Validation).
- **Limits.** `max_concurrent_threads_per_session` counts open children in the whole tree under the root session; finished children count until closed, as in Codex. Depth comes from the live children and then the sidecars, so a resumed child keeps its depth. `max_depth = 0` or `enabled = false` offers no tools and still answers past calls.
- **Approvals.** A child's session asks through the parent session's `engine.Options.AskAnytime`, with `agent <nickname>:` before the justification. Unlike a run's own prompts, such a prompt stays open after the parent's run ends and can be asked while the parent is idle; it ends when answered, when the child is interrupted or closed, or when the parent session closes. The child's own run applies the auto-reviewer first, with the child's own transcript.
- **Interrupts.** Interrupting the parent's run stops the live runs of its children and their descendants; they stay open as `interrupted`, and `send_input` starts them again. `send_input` with `interrupt: true` interrupts one child and hands it the message at once.
- **Resume.** `resume_agent(id)` reopens a closed child, or a child of an earlier process, when its sidecar names the calling session as its parent; it keeps its history, nickname, role, and model. `send_input` to such a child says to resume it first.
- **Hooks.** `SubagentStop` runs when a child completes, with Claude Code's payload (`agent_id`, `agent_type`, `agent_transcript_path`, `last_assistant_message`, and the parent's `session_id`); a block with a reason sends the reason to the child as its next message (at most 5 times in a row). Children run every other hook as the root session does; PermissionRequest runs in the parent session, where their approvals go.
- **Progress.** `engine.AgentUpdated` (state) and `engine.AgentActivity` (a child's tool events) go into the parent session's stream through `engine.Options.Notify`, so a child that finishes after the parent's run ended still updates its line. The TUI draws one line per child (`KindAgent`) with its latest 30 tool calls under it in the detailed view (ctrl+t); `/agents` lists the children; `uah run` prints `agent <nickname>: <state>`, and the tool calls with `--verbose`.
- **Roles** load from `~/.config/uagent/agents` and a trusted workspace's `.uagent/agents` (recursive `*.toml`, later directories win). The subset read: `name`, `description`, `nickname_candidates`, `model`, `model_reasoning_effort`, `developer_instructions`, with Codex's validation; other keys produce a warning notice.

## Validation

A review on 2026-09-24 against Codex rust-v0.156.1. Before any change, every `internal/agents` test passed 20 times under `-race`. Each finding below is fixed with a test unless it says otherwise.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | High | A child's approval was declined when the parent's run ended, and any child approval while the parent was idle was declined: the session accepted prompts only during a run. | Fixed: `engine.Options.AskAnytime`. Tests: `TestAgents_ChildApprovalOutlivesTheParentsRun`, `TestSession_AskAnytimeOutlivesTheRun`, `TestSession_AskAnytimeEndsWithItsContext`. |
| 2 | High | `close_agent` on a child waiting for approval hung until the harness killed the child's run: the prompt lived in the parent's session, and closing the child did not end it. | Fixed: a child's prompts end when it is interrupted or closed. Test: `TestAgents_CloseEndsAPendingApproval`. |
| 3 | Medium | The tools differed from Codex's v1: `wait` for `wait_agent`, `id`/`ids` for `target`/`targets`, `{id}` for `{agent_id}`, `{state, message}` for `AgentStatus`, `{id, status: "sent"}` for `{submission_id}`, `not_found` without an error from `close_agent`, `timeout_ms` ≤ 0 accepted, no `interrupt` on `send_input`, no `resume_agent`. | Fixed. Tests: `TestCall_Errors`, `TestStatus_JSON`, `TestTools_Offered`, and the end-to-end tests. |
| 4 | Medium | A long final answer pushed the `wait_agent` result over the runner's 40,000-character cap on a remote job's result; the runner cut the JSON in the middle and could drop other children's statuses. | Fixed: the messages share a 36,000-character budget, keeping each one's head and tail. Test: `TestAgents_LongAnswerKeepsTheResultValid`. |
| 5 | Medium | Progress updates could reach the parent out of order (the state was read under the lock and sent after it, from two goroutines), leaving a finished child drawn as running. | Fixed: one lock covers reading and sending an update. No deterministic test; the order holds by construction, and the tests pass 20 times under `-race`. |
| 6 | Medium | The auto-reviewer's transcript was one per engine: a child's messages joined the parent's, and the child's reviewer took over the parent's circuit-breaker reset. | Fixed: one transcript per session. Test: `TestTranscript_PerSession`. |
| 7 | Medium | Interrupting the parent left its children running, with no way for the user to stop them short of closing the session. | Fixed (a deliberate difference from Codex, whose TUI can interrupt each child): the parent's interrupt stops its children's live runs. Test: `TestAgents_InterruptStopsChildren`. |
| 8 | Low | A spawn racing the parent's Close could open a session nothing closed; a child whose first message failed stayed open and counted toward the limit; a spawn that finished after the parent's run stopped left the child running. | Fixed: the child is closed in each case. No deterministic test for the races. |
| 9 | Low | Children ran PreToolUse hooks (on the engine) but not PostToolUse hooks (in the session). | Fixed. Test: `TestAgents_SubagentStopHook`. |
| 10 | Low | `send_input` and `wait` could not reach children of an earlier process. | Fixed: `resume_agent`. Tests: `TestAgents_ResumeAcrossProcesses`, `TestAgents_ResumeOnlyOwnChildren`. |
| 11 | High | A child was built in a parallel, trimmed-down way: its settings were rebuilt from the parent's request and the configured base, so a fast mode switched on in the session did not reach it; it ran only PostToolUse of the session's hooks; its engine handle hid the MCP servers. | Fixed: a child opens with the root session's options from `app.Setup` and the parent run's settings, with only the differences listed in As built. Tests: `TestSetup_SubagentParity` (through `app.Setup`: the child's model request has the root's system prompt, model, effort, service tier, and tools, and Stop hooks run for both), `TestParity_ChildOptions`, `TestSettings_WithRequestInvertsRequest`. |
| 12 | Low | A child's end of work was found by counting queued messages; a message the session queued itself (a Stop hook's continuation) threw the count off, so an `Idle` from before a later message could end that message's work early. | Fixed: messages are tracked by ID. The interleaving has no deterministic test; the tests pass 20 times under `-race`. |
| 13 | — | Closing the parent closes every child, and job goroutines end with the run. | Verified: `TestAgents_CloseParentClosesChildren`; every test passes 20 times under `-race`. |

Differences from Codex, kept on purpose:

1. **Interrupts reach children.** Codex's interrupt stops only the parent's turn. uah has no per-child view to stop a child from, so the parent's interrupt stops its children too; they stay open.
2. **`interrupted` is final for `wait_agent`.** In Codex an interrupted agent is not final, so waiting on it runs to the timeout; in uah nothing but the parent's `send_input` starts it again, so `wait_agent` reports it at once.
3. **No completion notification.** Codex injects a `<subagent_notification>` into the parent's history when a child finishes. The runner's inbox (v0.1.1) takes only user messages, which would show as the user's input and start a turn; `wait_agent` returns a finished child at once instead, and the TUI line shows it.

Open:

1. **Stopping a child while the parent is idle.** The user can interrupt only a live parent run; an idle parent's children run until they finish, the model closes them, or the session closes.
2. **v2 tools**, `items`, and `fork_context` are not built (see Open decisions).
