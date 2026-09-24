<!-- memoria:section id="overview" files="session.go loop.go events.go" -->
# Sessions

A session is the long-lived object the TUI and `uah run` talk to. It owns the settings, a queue of messages, at most one live run, pending approvals, and the hooks, and it merges run events and its own events into one ordered stream.

<!-- memoria:export id="summary" -->
A session owns its settings, a message queue, at most one live run, pending approvals, and its hooks on one goroutine, and merges run events and its own events into one ordered stream. Messages queue while the agent works, a steer reaches the running agent when the engine allows it, and an interrupt keeps the queue.
<!-- /memoria:export -->

1. [The loop](#the-loop)
2. [Messages: queue, steer, interrupt](#messages-queue-steer-interrupt)
3. [Settings and compaction](#settings-and-compaction)
4. [Approvals](#approvals)
5. [Hooks](#hooks)
6. [Files and history](#files-and-history)
7. [Events](#events)
8. [Tests](#tests)
<!-- /memoria:section -->

<!-- memoria:section id="loop" files="session.go loop.go runs.go" -->
## The loop

`Open` starts one goroutine, `loop`, that owns all session state. Every public method (`Submit`, `SteerNow`, `Interrupt`, `Withdraw`, `SetSettings`, `Compact`, `Resolve`) sends a command on the `in` channel and waits for the reply (`call[T]`). Engine callbacks do the same: the run's sink, `Start`'s result, `Wait`'s result, hook results, and approval requests all arrive as messages. So nothing else needs a lock, and `Events()` has exactly one writer.

The loop tracks where the session is in a run:

| State | Means |
| --- | --- |
| `idle` | No run. A message starts one |
| `starting` | `Engine.Start` runs on another goroutine; the loop gets `evStarted` |
| `running` | A run is live; another goroutine waits on `Run.Wait` and sends `evEnded` |
| `stopping` | An interrupt was sent; the run has not ended yet |
| `closed` | `Close` finished: SessionEnd hooks ran and `Events()` is closed |

`Close` interrupts a live run, waits for it to end, closes the event stream, and then closes the engine when it is an `io.Closer` (MCP servers and subagents stop with the session).

`Open` also checks the engine's capabilities against `Options.Uses`, the features the configuration asks for: each one the engine does not run gets one `Notice` after `SessionOpened`, from the [capability table](../engine/README.md#what-each-engine-supports). The session never checks the engine's name.
<!-- /memoria:section -->

<!-- memoria:section id="messages" files="dispatch.go runs.go inject.go shell.go" -->
## Messages: queue, steer, interrupt

Every message gets an ID and is reported as `InputQueued`, then `InputSent` when it goes to the runner, and `InputDelivered` when the runner echoes it as a `UserMessage`. Messages that never reached the runner are reported as `InputFailed`.

What `dispatch` does with a message depends on the state and on whether it is a steer (ctrl+enter, `SteerNow`):

| State | Enter (`Submit`) | Steer (`SteerNow`) |
| --- | --- | --- |
| `idle` | Starts a run with the queue, then this message | The same |
| `running` | Queues it; the queue starts the next run | With `LiveInput`, sends it into the run. Otherwise queues it and interrupts; a new run starts with the queue |
| `starting` | Queues it | With `LiveInput`, holds it and sends it once the run has started. Otherwise queues it and interrupts |
| `stopping` | Queues it | Queues it; a new run starts after the stop |

When a run ends, messages sent into it that it never read go back to the front of the queue. After a user interrupt (esc esc, `/stop`) the queue stays and the session goes idle; otherwise the queue starts the next run at once. `Withdraw` takes a message back while it is queued or waiting for its hooks.

`Inject` gives the agent a message without a turn of its own, as Codex's `inject_no_new_turn`: it is held and goes out before the next run's messages, and it never starts a run. It is not sent into a live run, because the runner cancels its model request when a message arrives, which would throw away a paid request. It skips the queue and the hooks. A subagent's `<subagent_notification>` reaches its parent this way (`engine.Options.Inject`).

`RunShell(ctx, command)` runs a command the user typed (the TUI's `!` shell mode) with `Options.Shell`, a `usershell.Runner` that `internal/app` builds, so it works the same on both engines. It runs at once, in any state, also while a run is live, as Codex's user shell commands do. `ShellStarted`, `ShellOutput`, and `ShellFinished` report it. The session's interrupt stops it, and so does `Close`. Its record, Codex's `<user_shell_command>` message with the command, exit code, duration, and output cut to 40,000 characters, is held as `Inject` holds a message, with the command's ID: it goes before the next run's messages and never starts a run. A failed, stopped, or refused command is recorded too. By default the command runs outside the sandbox and the command rules, as in Codex; `user_shell_sandbox = true` runs it in the sandbox of the current permission mode and lets a `forbidden` rule refuse it. The [shell mode design](../../docs/design/shell-mode.md) has the research and the decisions.
<!-- /memoria:section -->

<!-- memoria:section id="settings" files="settings.go dispatch.go compact.go saved.go" -->
## Settings and compaction

`Settings` are what every run sends: provider, model, effort, service tier, the permission mode, workspace, and the host prompt. The permission mode (`approval.Mode`: read only, workspace, auto, or full access) goes to the engine as `engine.Options.Mode`; `WithMode` sets it and keeps `Sandbox`, its sandbox mode for display, in step. `SetSettings` stores them and, while a run is live, tries `SetEffort`, `SetModel`, `SetServiceTier`, and `SetMode` on it. `SettingsChanged.Applied` says `live` when all changed fields reached the run, and `next_run` otherwise (always on the process engine).

The session keeps its provider, model, effort, fast mode, and permission mode in its sidecar (`Saved`): when it opens and after each change. On resume, `ApplySidecar` puts them in the session's `Info` in place of its newest run's provider, model, and effort, and `internal/app` restores them ahead of the configuration; a flag still wins. A session whose sidecar has no settings (from before uah kept them) resumes with its newest run's. A subagent's sidecar keeps its own settings.

`Compact` marks a compaction as pending; `CompactWith(focus)` adds what the summary should focus on (`/compact <focus>`). While a run is live, the engine compacts before its next model request (`Run.Compact(focus)`); while idle, `engine.Options.Compact` and `CompactFocus` ask the next run to compact first. The pending flag clears when the engine reports a manual `CompactionStarted`. `Clear` (`/clear`) works the same way with `Run.Clear` and `engine.Options.Clear`: the model's next request starts fresh in the same session. The process engine returns `ErrNoCompaction` for both.
<!-- /memoria:section -->

<!-- memoria:section id="approvals" files="approvals.go" -->
## Approvals

The session gives each run an `approval.Ask` (`askFunc`), which the engine calls when a command or an MCP call needs approval. It chooses, in order:

1. `Options.Ask`, when set. A subagent asks through its parent's session this way.
2. PermissionRequest hooks, when configured. The prompt reaches them as a tool call: `tool_name` is `Bash` with the command, or the `mcp__` name with its arguments. "allow" approves and "deny" (or exit 2) declines. A hook that decides neither passes the prompt on.
3. The user, when the session is interactive (the TUI). The ask hands the prompt to the loop, which emits `ApprovalRequested` and waits for `Resolve` with the same ID.
4. Otherwise nil or a decline: no one can answer in `uah run`.

The agent waits while an approval is open. An interrupt, the end of the run, or `Close` declines every pending approval of the run (`ApprovalResolved` with `decline`), so a waiting run can always stop. `engine.Options.AskAnytime` asks outside a run: a subagent's approval shows in its parent's session even while the parent is idle, and stays open until it is answered, its context ends, or the session closes. The rules and the auto-reviewer run before this ask; see [the permission pipeline](../approval/README.md).
<!-- /memoria:section -->

<!-- memoria:section id="hooks" files="hooks.go dispatch.go runs.go" -->
## Hooks

The session runs the hooks of its events; `internal/hooks` runs the commands. A single worker goroutine runs hook jobs one at a time, in order, and posts each decision back to the loop, so a slow hook never blocks the loop.

| Event | Where the session runs it |
| --- | --- |
| `SessionStart` | In `Open`, with `source` startup or resume. Its context is added to the first message |
| `UserPromptSubmit` | Before each message is dispatched. While hooks run, the message waits in `checking`; later messages wait behind it, so order is kept. A block reports `InputFailed` |
| `PostToolUse` | After each `ToolFinished`; it only observes |
| `Stop` | When a run ends with nothing queued. A block with a reason sends the reason as the next message, at most 5 times in a row. A new message cancels a pending Stop decision |
| `SessionEnd` | In `Close`, with at most a second per hook |
| `PermissionRequest` | In the approval ask, above |

These hooks are the same on both engines. PreToolUse and PreCompact hooks run in the embedded engine, on the coordinator's goroutine, and the process engine runs neither. Each hook run is reported as `HookRan`. The hook contract and trust are in [internal/hooks](../hooks/README.md).
<!-- /memoria:section -->

<!-- memoria:section id="files" files="sidecar.go history.go agentwatch.go patches.go" -->
## Files and history

The runner's session files and uagent's run records are the source of truth; the [state storage record](../../docs/design/state.md) lists every file and why the index is only a cache.

- **Sidecar.** A new session writes `sessions/<id>.uah.json` with its `source` (`tui`, `run`, or `subagent`), its creation time, and, for a subagent, its `parent`. The first writer wins (`O_EXCL`). Its `settings` are rewritten whole (a temporary file, then a rename) whenever they change; see [Settings and compaction](#settings-and-compaction). `Interactive` drops `run` and `subagent` sessions from the resume picker, as Codex hides `codex exec` sessions, and `Tree` lists subagents under their parents.
- **Subagent IDs.** `NewSubagentID` is `subagent-<uuid>`; `ShortID` prints `subagent-` and 8 characters of the UUID (8 characters for other sessions), a prefix that resumes the session. Older subagents have plain UUIDs; the sidecar's `parent` identifies them.
- **Watching a subagent.** `WatchAgent(ref)` follows one of the session's subagents, by ID or nickname, through the engine's `Subagents()` when it implements `AgentWatcher`: its earlier runs, its events so far, the ones that follow, and a way to message it. The TUI's agent view uses it; [internal/agents](../agents/README.md#watching-an-agent) implements it.
- **History.** `Sessions` folds run records into one `Info` per session, reading only summaries and the first request. `Load` reads every run of a session in start order with its events, which is how the TUI rebuilds a resumed transcript; each compaction saved in the compaction log is added to the run it happened in, in time order, so reloaded transcripts, `uah sessions show`, and `uah run --stream` show it. Likewise each applied `apply_patch` call's diff, read from its completed job in the run's events file (`patches.go`), follows the call as `engine.PatchApplied`. `InDir` matches a session's workspace the way Codex does: absolute, cleaned, and with symlinks resolved.

Listing and search go through the rebuildable SQLite index in [internal/store](../store/README.md), which falls back to `Sessions` when the index cannot be used.
<!-- /memoria:section -->

<!-- memoria:section id="events" files="events.go approvals.go shell.go" -->
## Events

`Events()` carries the runner's events (from uagent's `core`), the engine's events, and these session events:

| Event | Means |
| --- | --- |
| `SessionOpened` | Always first: the ID, whether it was resumed, the engine, and the settings |
| `InstructionsLoaded` | The instruction files in the host prompt |
| `InputQueued`, `InputSent`, `InputDelivered`, `InputFailed`, `InputWithdrawn` | A message's way to the runner |
| `SettingsChanged` | New settings, and whether they applied live |
| `ApprovalRequested`, `ApprovalResolved` | An approval waiting for `Resolve`, and its answer |
| `HookRan` | A hook's outcome, command, and duration |
| `Notice` | Text for the user, with a level |
| `ShellStarted`, `ShellOutput`, `ShellFinished` | A command the user typed (`RunShell`): its start, its output as it arrives, and its result with the record the agent gets |
| `Idle` | The session has nothing to do |

`uah run --stream` writes them as JSONL, and the TUI reduces them into its state.
<!-- /memoria:section -->

<!-- memoria:section id="tests" files="session_test.go hooks_test.go process_test.go compact_test.go history_test.go sidecar_test.go saved_test.go shell_test.go" -->
## Tests

`session_test.go` and `hooks_test.go` drive a session with a scripted fake engine, so each state transition can be held open: queueing while running, steering while starting, interrupts that keep the queue, messages the run never read, withdrawal, live settings, failures, and every hook event. `saved_test.go` pins the settings kept in the sidecar and a live mode change. `process_test.go` runs a session on the real process engine with uagent's fake runner: a steer that restarts the run, and what the session does the same on both engines (the host prompt, the session-level hooks, the saved settings and when they apply, and the notices for what the engine does not run). Resuming with the saved settings is tested end to end in `internal/app/resume_test.go`. `shell_test.go` pins `RunShell`: no run starts, the record goes first with the next message (on the fake engine and on the process engine), a command during a live run is not sent into it, and an interrupt stops it; `internal/app/usershell_test.go` runs it on the embedded engine with `testing/fakellm` (the next request carries Codex's format) and with `user_shell_sandbox` (a `forbid` rule refuses, the mode picks the sandbox), and `internal/usershell` tests a write outside the workspace failing in workspace mode.
<!-- /memoria:section -->
