<!-- memoria:section id="overview" files="manager.go tools.go" -->
# Subagents

<!-- memoria:export id="summary" -->
Subagents are child sessions that a session's agent starts, messages, waits for, and closes through Codex's v1 multi-agent tools. `internal/agents` implements them behind the `engine.Subagents` seam: the embedded engine offers the tools and runs their calls in the background, and the package owns the tools, the children's lifecycle, approvals through the parent, limits, hooks, and resume.
<!-- /memoria:export -->

This README describes how the package works for someone changing it. The root README describes the feature for users, and [the design record](../../docs/design/subagents.md) keeps the research, the decisions, and the validation findings.

1. [The seam](#the-seam)
2. [A child's lifecycle](#a-childs-lifecycle)
3. [The tools](#the-tools)
4. [Approvals](#approvals)
5. [Events and hooks](#events-and-hooks)
6. [Persistence and resume](#persistence-and-resume)
7. [Limits](#limits)
8. [Roles](#roles)
9. [Extending](#extending)
<!-- /memoria:section -->

<!-- memoria:section id="seam" files="manager.go tools.go" -->
## The seam

`engine.Subagents` (in `internal/engine/subagents.go`) is the only contract between the engine and this package. `Manager` implements it:

- `Attach` runs at the start of each of a session's runs on the embedded engine. It records the run as a parent (its request, how to ask its user, and how to add events to its stream) and returns the tools to offer, or none when the session is too deep to spawn.
- `ToolNames` lists every name a call may use, offered or not. The engine resolves these names in every run, so a session with past calls resumes where the tools are not offered.
- `Call` runs one tool call with its JSON arguments and returns the JSON result for the model. The engine runs each call as a remote job (`uah.agent` v1) on its own goroutine, so a long `wait_agent` never holds up the parent's coordinator. The call's context ends when the coordinator cancels the call or the run stops.
- `Interrupt` runs when the user interrupts the parent's run.

The engine knows no tool name, schema, or result, and the session knows children only as sessions with `source: "subagent"` and a `parent` in their sidecar. `app.Setup` builds the manager for the embedded engine, even when subagents are off, and binds it to that engine with `Bind`. `Close` closes every child; the engine calls it when a session on it closes, and the manager stays usable for the engine's next session.
<!-- /memoria:section -->

<!-- memoria:section id="lifecycle" files="child.go ops.go status.go" -->
## A child's lifecycle

A child is a `session.Session` on the parent's engine. The engine is wrapped so that closing a child leaves the shared MCP servers running. The manager's lock guards every child's fields.

1. **Start.** `start` checks the limit, registers the child, and opens its session with the parent run's provider, model, effort, workspace, and host prompt. The configured defaults, the role, and the spawn call override the model and effort in that order, and a role's `developer_instructions` follow the host prompt. A goroutine, `watch`, then follows the session's events until it closes.
2. **Running.** `submit` sends a message and marks the child `running`. The session queues the message while a run is live, so a child reads a second message after its current run.
3. **Finished.** When the session reports `Idle` and has accepted every message sent (`queued` catches up with `inputs`, so an `Idle` from before a message does not count), the child is `completed` with the last run's answer, `interrupted`, or `errored`. With `SubagentStop` hooks, a completed child first runs them (see [Events and hooks](#events-and-hooks)).
4. **Closed.** `close_agent`, closing the parent, or `Close` closes the child's session and its open descendants. The watcher then reports `shutdown`.

A status encodes as Codex's `AgentStatus`: a string (`pending_init`, `running`, `interrupted`, `shutdown`, `not_found`) or `{"completed": message}` and `{"errored": message}`. Every status but `pending_init` and `running` is final. Unlike in Codex, `interrupted` is final, because only the parent's `send_input` starts such a child again.

Interrupting the parent's run calls `Interrupt`, which stops the live runs of the parent's children and their descendants. They stay open as `interrupted`.
<!-- /memoria:section -->

<!-- memoria:section id="tools" files="tools.go prompt.go ops.go status.go" -->
## The tools

The tools are Codex's v1 set (rust-v0.156.1), with its parameters, results, and error messages. `prompt.go` holds the descriptions and schemas, adapted from Codex under the Apache License 2.0.

| Tool | Arguments | Result |
| --- | --- | --- |
| `spawn_agent` | `message`, `agent_type?`, `model?`, `reasoning_effort?` | `{agent_id, nickname}` at once |
| `send_input` | `target`, `message`, `interrupt?` | `{submission_id}` |
| `wait_agent` | `targets`, `timeout_ms?` (default 30 s, 10 s to 1 h) | `{status: {id: status}, timed_out}` |
| `close_agent` | `target` | `{previous_status}` |
| `resume_agent` | `id` | `{status}` |

- **spawn_agent** starts a child with its first message and returns while the child works.
- **send_input** queues the message. With `interrupt`, it stops the child's live run first and hands the child the message at once.
- **wait_agent** returns as soon as any listed child is in a final status, with every listed child that is final by then. An unknown ID is final as `not_found`. The final messages share a budget of 36,000 characters, keeping each one's head and tail, so the result stays valid JSON under the runner's 40,000-character cap on a remote job's result.
- **close_agent** closes the child and its descendants and returns the status it had before.
- **resume_agent** reopens a closed child (see [Persistence and resume](#persistence-and-resume)).

`items` (structured input) and `fork_context` (a child that starts from the parent's history) are left out; the runner cannot do them without changes.
<!-- /memoria:section -->

<!-- memoria:section id="approvals" files="ask.go" -->
## Approvals

A child's session has no user of its own. Its `session.Options.Ask` asks through the parent session's `engine.Options.AskAnytime`, with `agent <nickname>:` before the justification. The child's own run applies the auto-reviewer first, on the child's own transcript; what it leaves to the user reaches the parent session and its PermissionRequest hooks.

Such a prompt stays open after the parent's run ends, and a child can ask while the parent is idle. It ends when the user answers, when the child is interrupted or closed (each child has a context for its prompts that these cancel), or when the parent session closes. With no one to ask, as in `uah run`, the child is declined with a reason.
<!-- /memoria:section -->

<!-- memoria:section id="events" files="child.go stop.go" -->
## Events and hooks

The manager adds two engine events to the parent session's stream through the parent's `Emit`, which is `engine.Options.Notify`, so they arrive after the parent's run ended too:

- `engine.AgentUpdated`: the child's state, whenever it changes. One lock covers reading the state and sending it, so updates arrive in order.
- `engine.AgentActivity`: each of the child's tool events (`core.ToolCalled`, `core.ToolStarted`, `core.ToolFinished`), for the TUI's detailed view.

Waiters sleep on a channel that is closed and replaced at every change.

Hooks come from `Config.Hooks`, the session's runner:

- **SubagentStop** runs when a child completes, with Claude Code's payload: the parent's `session_id` and `transcript_path`, and the child's `agent_id`, `agent_type` (its role, or `default`), `agent_transcript_path`, and `last_assistant_message`. A block with a reason sends the reason to the child as its next message, at most 5 times in a row (`stop_hook_active` is true after the first). The child stays `running` while the hooks run; a message from the parent meanwhile makes their decision moot.
- **PreToolUse** runs on the engine for every session, children included. **PostToolUse** runs in the child's session, which gets a runner with only those hooks (`hooks.Runner.Only`). SessionStart, UserPromptSubmit, Stop, and SessionEnd do not run for children.
<!-- /memoria:section -->

<!-- memoria:section id="resume" files="record.go ops.go" -->
## Persistence and resume

A child is an ordinary session: its runner files, run records, and sidecar (`source: "subagent"`, `parent`) persist, `uah sessions` lists it under its parent, and the resume picker hides it. The manager also writes `sessions/<id>.agent.json` with the nickname, the role, and the spawn call's model and effort.

The manager knows only the children of the current process. `resume_agent(id)` reopens a closed child, or a child of an earlier process, when its sidecar names the calling session as its parent. The child keeps its history, nickname, role, and model, and reports `pending_init` until it gets a message. `send_input` and `close_agent` to such a child say to resume it first; `wait_agent` reports it as `not_found`, as Codex does. A call that had started before a restart fails instead of running twice.
<!-- /memoria:section -->

<!-- memoria:section id="limits" files="manager.go ops.go" -->
## Limits

- **Concurrency.** `MaxThreads` (`max_concurrent_threads_per_session`, default 4) counts the open children in the whole tree under the root session, checked under the lock so parallel spawns keep it. Finished children count until closed, as in Codex.
- **Depth.** `MaxDepth` (default 1: children cannot spawn) is compared with a session's depth, which comes from the live children and then the sidecars, so a resumed child keeps its depth. `MaxDepth` 0 offers no tools; `app.Setup` uses it when `[agents] enabled = false`, and past calls still get an answer.
<!-- /memoria:section -->

<!-- memoria:section id="roles" files="roles.go" -->
## Roles

Agent types are Codex role files, loaded by `LoadRoles` from `~/.config/uagent/agents` and a trusted workspace's `.uagent/agents` (recursive `*.toml`; a later directory replaces a role of the same name). The subset read is `name`, `description`, `nickname_candidates`, `model`, `model_reasoning_effort`, and `developer_instructions`, with Codex's validation. Other keys produce a warning, and a malformed file is skipped with one. The `spawn_agent` description lists the roles.
<!-- /memoria:section -->

<!-- memoria:section id="extending" files="tools.go prompt.go roles.go" -->
## Extending

- **A tool.** Add an entry to `tools` in `tools.go` with its name, description, JSON Schema, and run function, and its text to `prompt.go`. The engine offers and runs it with no change. Keep a removed tool's name in `ToolNames`, as `wait` is kept, so sessions with past calls resume.
- **A tool set, such as Codex's v2.** Choose the set in `definitions` and `Call` by configuration; the seam and the engine stay the same.
- **A role key.** Add the field to `Role` with its TOML name, apply it in `Manager.settings` (or where it belongs), and document it in the root README. `LoadRoles` warns about any key `Role` does not decode.
<!-- /memoria:section -->
