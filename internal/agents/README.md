<!-- memoria:section id="overview" files="manager.go tools.go" -->
# Subagents

<!-- memoria:export id="summary" -->
Subagents are child sessions that a session's agent starts, messages, waits for, and closes through Codex's v1 multi-agent tools. `internal/agents` implements them behind the `engine.Subagents` seam: the embedded engine offers the tools and runs their calls in the background, and the package owns the tools, the children's lifecycle, approvals through the parent, limits, hooks, and resume.
<!-- /memoria:export -->

This README describes how the package works for someone changing it. The root README describes the feature for users, and [the design record](../../docs/design/subagents.md) keeps the research, the decisions, and the validation findings.

1. [The seam](#the-seam)
2. [Parity with the root session](#parity-with-the-root-session)
3. [A child's lifecycle](#a-childs-lifecycle)
4. [The tools](#the-tools)
5. [Approvals](#approvals)
6. [Events and hooks](#events-and-hooks)
7. [Forking](#forking)
8. [Watching an agent](#watching-an-agent)
9. [Persistence and resume](#persistence-and-resume)
10. [Limits](#limits)
11. [Roles](#roles)
12. [Extending](#extending)
<!-- /memoria:section -->

<!-- memoria:section id="seam" files="manager.go tools.go" -->
## The seam

`engine.Subagents` (in `internal/engine/subagents.go`) is the only contract between the engine and this package. `Manager` implements it:

- `Attach` runs at the start of each of a session's runs on the embedded engine. It records the run as a parent (its request, how to ask its user, and how to add events to its stream) and returns the tools to offer, or none when the session is too deep to spawn.
- `ToolNames` lists every name a call may use, offered or not. The engine resolves these names in every run, so a session with past calls resumes where the tools are not offered.
- `Call` runs one tool call (`engine.AgentCall`: the parent's ID, the model's call ID, the tool, and its JSON arguments) and returns the JSON result for the model. The engine runs each call as a remote job (`uah.agent` v1) on its own goroutine, so a long `wait_agent` never holds up the parent's coordinator. The call's context ends when the coordinator cancels the call or the run stops.
- `Interrupt` runs when the user interrupts the parent's run.

The engine knows no tool name, schema, or result, and the session knows children only as sessions with `source: "subagent"` and a `parent` in their sidecar. An engine that also implements `engine.Forker` (the embedded engine) copies a parent's history into a child for `fork_context` and gives every child its tree's prompt cache key (see [Forking](#forking)). `app.Setup` builds the manager for the embedded engine, even when subagents are off, and binds it with `Bind` to that engine and to the session options it returns, the ones the root session opens with. `Close` closes every child; the engine calls it when a session on it closes, and the manager stays usable for the engine's next session.
<!-- /memoria:section -->

<!-- memoria:section id="parity" files="manager.go ops.go" -->
## Parity with the root session

A subagent is the root agent in every way except what makes it a child. `childOptions` starts from the options `app.Setup` returned for the root session and opens the child with `session.Open` on the same engine, so the child gets the same instructions and skills, sandbox, rules, approvals policy and auto-review, hooks, MCP servers, compaction, context meter, tool output limits, index, and sidecar. Its settings are the parent run's: `Settings.WithRequest` inverts the request the parent's run started with, and the run's service tier follows.

The only differences:

1. its session ID, `subagent-<uuid>`, and its sidecar's `source: "subagent"` and `parent`;
2. the spawn tools, offered only below `max_depth`, except to a forked child;
3. approvals, asked through the parent session;
4. the model, effort, service tier, and instructions a role, the spawn call, or `[agents]` defaults set;
5. a hook runner of its own with the same hooks, so hook results stay with the child's session;
6. its engine handle, which does not close the shared engine when the child closes;
7. its prompt cache key, the root session's ID, as Codex keys every agent of a tree.

`TestSetup_SubagentParity` (in `internal/app`) and `TestParity_ChildOptions` pin this: a child's model request has the root's system prompt, model, effort, service tier, and tools with the same schemas, less the spawn tools, and its options equal the root's but for the differences above. A capability added to the root session reaches children without a change here.
<!-- /memoria:section -->

<!-- memoria:section id="lifecycle" files="child.go ops.go status.go cause.go" -->
## A child's lifecycle

A child is a `session.Session` on the parent's engine. The engine is wrapped so that closing a child leaves the shared MCP servers running. The manager's lock guards every child's fields.

1. **Start.** `spawn` checks the depth and, on the openai-codex provider, the model (see [The tools](#the-tools)). `start` checks the limit, registers the child with the ID `subagent-<uuid>`, sets its prompt cache key, and opens its session with `childOptions` (see [Parity](#parity-with-the-root-session)). The spawn call, the role, and the configured defaults override the model and effort in that order, a role's `service_tier` sets fast mode, and a role's `developer_instructions` follow the host prompt. With `fork_context`, the engine copies the parent's history into the child's session now (see [Forking](#forking)). A goroutine, `watch`, then follows the session's events until it closes.
2. **Running.** `submit` sends a message and marks the child `running`. The session queues the message while a run is live, so a child reads a second message after its current run.
3. **Finished.** When the session reports `Idle` and has queued every message sent (tracked by message ID, so an `Idle` from before a message, or a message the session added itself, such as a Stop hook's, does not count), the child is `completed` with the last run's answer, `interrupted`, or `errored`. An errored child's message is the cause in one line (`cause.go`): the provider's message from its JSON error body when the run's `RunnerError` has one, such as `The 'gpt-luna-6' model is not supported when using Codex with a ChatGPT account.`, else the error itself. With `SubagentStop` hooks, a completed child first runs them (see [Events and hooks](#events-and-hooks)).
4. **Closed.** `close_agent`, closing the parent, or `Close` closes the child's session and its open descendants. The watcher then reports `shutdown`.

A status encodes as Codex's `AgentStatus`: a string (`pending_init`, `running`, `interrupted`, `shutdown`, `not_found`) or `{"completed": message}` and `{"errored": message}`. Every status but `pending_init` and `running` is final. Unlike in Codex, `interrupted` is final, because only the parent's `send_input` starts such a child again.

Interrupting the parent's run calls `Interrupt`, which stops the live runs of the parent's children and their descendants. They stay open as `interrupted`.
<!-- /memoria:section -->

<!-- memoria:section id="tools" files="tools.go prompt.go ops.go status.go" -->
## The tools

The tools are Codex's v1 set (rust-v0.156.1), with its parameters, results, and error messages. `prompt.go` holds the descriptions and schemas, adapted from Codex under the Apache License 2.0.

| Tool | Arguments | Result |
| --- | --- | --- |
| `spawn_agent` | `message`, `agent_type?`, `fork_context?`, `model?`, `reasoning_effort?` | `{agent_id, nickname}` at once |
| `send_input` | `target`, `message`, `interrupt?` | `{submission_id}` |
| `wait_agent` | `targets`, `timeout_ms?` (default 30 s, 10 s to 1 h) | `{status: {id: status}, timed_out}` |
| `close_agent` | `target` | `{previous_status}` |
| `resume_agent` | `id` | `{status}` |

- **spawn_agent** starts a child with its first message and returns while the child works. With `fork_context`, the child starts from a copy of the parent's history (see [Forking](#forking)); a fork keeps the parent's agent type, so `agent_type` with `fork_context` is refused with Codex's message. At the depth limit it returns Codex's `Agent depth limit reached. Solve the task yourself.` A `model` (or `default_subagent_model`) the provider does not offer is refused with Codex's `Unknown model ... Available models: ...` and a did-you-mean suggestion. `Config.Validate` does the check (`models.go`); `app.Setup` sets it to [internal/models](../models/README.md)'s `Validate`, which uses the provider's live list, so it works on every provider that lists its models.
- **send_input** queues the message. With `interrupt`, it stops the child's live run first and hands the child the message at once.
- **wait_agent** returns as soon as any listed child is in a final status, with every listed child that is final by then. An unknown ID is final as `not_found`. The final messages share a budget of 36,000 characters, keeping each one's head and tail, so the result stays valid JSON under the runner's 40,000-character cap on a remote job's result.
- **close_agent** closes the child and its descendants and returns the status it had before.
- **resume_agent** reopens a closed child (see [Persistence and resume](#persistence-and-resume)).

`items` (structured input) is left out: the runner's inbox takes plain text.
<!-- /memoria:section -->

<!-- memoria:section id="approvals" files="ask.go" -->
## Approvals

A child's session has no user of its own. Its `session.Options.Ask` asks through the parent session's `engine.Options.AskAnytime`, with `agent <nickname>:` before the justification. The child's own run applies the auto-reviewer first, on the child's own transcript; what it leaves to the user reaches the parent session and its PermissionRequest hooks.

Such a prompt stays open after the parent's run ends, and a child can ask while the parent is idle. It ends when the user answers, when the child is interrupted or closed (each child has a context for its prompts that these cancel), or when the parent session closes. With no one to ask, as in `uah run`, the child is declined with a reason.
<!-- /memoria:section -->

<!-- memoria:section id="events" files="child.go stop.go" -->
## Events and hooks

The manager adds two engine events to the parent session's stream through the parent's `Emit`, which is `engine.Options.Notify`, so they arrive after the parent's run ended too:

- `engine.AgentUpdated`: the child's state, whenever it changes. One lock covers reading the state and sending it, so updates arrive in order. Each update is the whole picture: `ID`, `Nickname`, `Role`, `State`, `Message` (why an errored child failed), `Started`, the spawn call's `CallID` and message (`Task`), the child's `Model` and `Effort`, and `Forked`. The TUI keeps the latest one in its `KindAgent` item (`Item.Agent`), so a view can map an agent ID to its nickname and a spawn call to its agent.
- `engine.AgentActivity`: each of the child's tool events (`core.ToolCalled`, `core.ToolStarted`, `core.ToolFinished`), for the TUI's detailed view.

Waiters sleep on a channel that is closed and replaced at every change.

**Notifications.** When a child reaches a final status (completed, errored, or interrupted), the parent's agent is told with Codex's v1 message, a user-role `<subagent_notification>` holding `{"agent_path": <child ID>, "status": <status>}` (Codex `core/src/agent/control.rs`, `SubagentNotification`). It goes through `AgentParent.Inject`, which is `Session.Inject`: it waits and goes out with the parent's next message, starting no run, as Codex's `inject_no_new_turn`. It is not sent into a live run, since the runner would cancel that run's model request; a parent that needs the status at once has `wait_agent`. Once per message the child was sent (`completionNote`). Two cases send none, because the parent learns the status anyway: a child the parent closed itself, and a child a pending `wait_agent` covers (`child.waiters`); Codex sends it in both, so a waiting parent there sees the status twice.

Hooks come from `Config.Hooks`, the session's runner:

- **SubagentStop** runs when a child completes, with Claude Code's payload: the parent's `session_id` and `transcript_path`, and the child's `agent_id`, `agent_type` (its role, or `default`), `agent_transcript_path`, and `last_assistant_message`. A block with a reason sends the reason to the child as its next message, at most 5 times in a row (`stop_hook_active` is true after the first). The child stays `running` while the hooks run; a message from the parent meanwhile makes their decision moot.
- **Every other hook** runs for a child as for the root session: PreToolUse on the engine, and SessionStart, UserPromptSubmit, PostToolUse, Stop, PreCompact, and SessionEnd in the child's session, which has its own runner (`hooks.Runner.Clone`). PermissionRequest runs in the parent session, where the child's approvals go.
<!-- /memoria:section -->

<!-- memoria:section id="fork" files="ops.go manager.go record.go" -->
## Forking

`spawn_agent` with `fork_context: true` starts the child from a copy of the parent's history, as Codex's v1 tool does. The purpose is the provider's prompt cache: work that needs the current context starts without exploring again, and its first request shares the longest possible prefix with the parent's.

1. **The copy.** `engine.Forker.Fork(parent, child, callID)` finds the parent's model response that made the spawn call, and copies every item of the parent's session before that turn into the child's new session, through the runner's session store (`AppendInput`, `AppendTurn`, `AppendModelResponse`, `AppendToolCallStatus`). These items are the input of the parent's request that made the call. The runner's own `Store.Fork` (v0.1.1) is not used: it drops the operation snapshots of inherited tool calls, so their results would be missing.
2. **Unfinished work.** A copied tool call whose operation had not ended is recorded as canceled for the child, so the child never runs the parent's work again.
3. **Compaction.** The parent's compactions recorded before that response are copied to `sessions/<child>.compaction.jsonl`, so the child's request is compacted as the parent's was.
4. **The first run.** The engine puts the child's first messages and its effort in the store before the coordinator restores the session, because restoring counts the copied inputs as undelivered and asks the model at once. The inbox then drops the messages as already seen.
5. **The same prefix.** The child's system prompt is the parent's (a fork has no role instructions of its own), and its tools are the parent's in the same order: a forked child is offered the spawn tools even at the depth limit, and its spawn and resume calls are refused there, as Codex refuses them. Every child uses the root session's ID as its prompt cache key (the `prompt_cache_key` field, and the `session-id` header on openai-codex), as Codex keys all agents of a tree by the root session.

`TestFork_ChildStartsWithTheParentsRequest` and `TestFork_KeepsTheParentsCompaction` check with fakellm that the child's first request has the parent's system prompt, tools, and cache key, and starts with every input item of the parent's request, tool calls and results included, followed by the child's message.
<!-- /memoria:section -->

<!-- memoria:section id="watch" files="watch.go" -->
## Watching an agent

`Manager` implements `session.AgentWatcher`, and `Session.WatchAgent(ref)` reaches it through the embedded engine's `Subagents()`. A watch finds the parent's child by ID or nickname and returns:

- `History`: the child's runs from before this process, from its run records;
- `Events`: its session's events since it opened in this process, which the watcher logs (the newest 20,000);
- `Next`: the events that follow, on a channel of 4,096. A view that falls a whole queue behind is closed rather than holding up the child; the TUI opens it again;
- `Send`: a message to the child through `submit`, as `send_input` sends it, so the child's status and `wait_agent` see it;
- `Interrupt`: stops the child's current work and its own children's (`interruptTree`, which the parent's `Interrupt` also uses for each child); the child stays open;
- `Stop`: the end of the watch.

The TUI's `/agents <name>` view is built on it (see the TUI README).
<!-- /memoria:section -->

<!-- memoria:section id="resume" files="record.go ops.go" -->
## Persistence and resume

A child is an ordinary session: its runner files, run records, and sidecar (`source: "subagent"`, `parent`) persist, `uah sessions` lists it under its parent, and the resume picker hides it. Its ID is `subagent-<uuid>`: the runner's session store, uagent's session lock, and the run records accept ASCII letters, digits, and dashes. `uah sessions` prints `subagent-` and the first 8 characters of the UUID, a prefix that `uah resume` and `uah sessions show` find. Children from before the prefix have plain UUIDs; their sidecar's `parent` identifies them, as it identifies every child. The manager also writes `sessions/<id>.agent.json` with the nickname, the role, the spawn call's ID, message, model, and effort, and whether the child was forked.

The manager knows only the children of the current process. `resume_agent(id)` reopens a closed child, or a child of an earlier process, when its sidecar names the calling session as its parent. The child keeps its history, nickname, role, and model, and reports `pending_init` until it gets a message. `send_input` and `close_agent` to such a child say to resume it first; `wait_agent` reports it as `not_found`, as Codex does. A call that had started before a restart fails instead of running twice.
<!-- /memoria:section -->

<!-- memoria:section id="limits" files="manager.go ops.go" -->
## Limits

- **Concurrency.** `MaxThreads` (`max_concurrent_threads_per_session`, default 4) counts the open children in the whole tree under the root session, checked under the lock so parallel spawns keep it. Finished children count until closed, as in Codex.
- **Depth.** `MaxDepth` (default 1: children cannot spawn) is compared with a session's depth, which comes from the live children and then the sidecars, so a resumed child keeps its depth. A session at the limit is not offered the tools, as in Codex, except a forked child, which keeps its parent's tools; a spawn or resume from a session at the limit is refused with Codex's message. `MaxDepth` 0 offers no tools; `app.Setup` uses it when `[agents] enabled = false`, and past calls still get an answer.
<!-- /memoria:section -->

<!-- memoria:section id="roles" files="roles.go models.go" -->
## Roles

Agent types are Codex role files, loaded by `LoadRoles` from `~/.config/uagent/agents` and a trusted workspace's `.uagent/agents` (recursive `*.toml`; a later directory replaces a role of the same name). The subset read is `name`, `description`, `nickname_candidates`, `model`, `model_reasoning_effort`, `service_tier`, and `developer_instructions`, with Codex's validation. Other keys produce a warning, and a malformed file is skipped with one. The `spawn_agent` description lists the roles.

`service_tier` is Codex's key: `"priority"` (or its legacy name `"fast"`) runs the role's agents with priority processing when the provider offers it, `"default"` runs them without it, and an absent key follows the parent. `"flex"`, which no provider of uah's serves, is ignored with a warning. Codex rust-v0.156.1 reads the key into the role's config layer but then sets every child's tier to the root's (`apply_spawn_agent_service_tier`); uah applies the role's tier, so a role can turn fast mode on for its agents alone.
<!-- /memoria:section -->

<!-- memoria:section id="extending" files="tools.go prompt.go roles.go" -->
## Extending

- **A tool.** Add an entry to `tools` in `tools.go` with its name, description, JSON Schema, and run function, and its text to `prompt.go`. The engine offers and runs it with no change. Keep a removed tool's name in `ToolNames`, as `wait` is kept, so sessions with past calls resume.
- **A tool set, such as Codex's v2.** Choose the set in `definitions` and `Call` by configuration; the seam and the engine stay the same.
- **A role key.** Add the field to `Role` with its TOML name, apply it in `Manager.settings` (or where it belongs), and document it in the root README. `LoadRoles` warns about any key `Role` does not decode.
<!-- /memoria:section -->
