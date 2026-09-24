<!-- memoria:section id="overview" files="engine.go capabilities.go events.go process/process.go embedded/engine.go" -->
# Engines

An engine starts runs of unreal-agent-runner for a session. The `process` engine spawns the runner binary through uagent; the `embedded` engine runs the runner's own packages inside uah, so messages and settings reach a live run.

<!-- memoria:export id="summary" -->
An engine starts runs of unreal-agent-runner for a session: the embedded engine (the default) runs the runner's packages inside uah, so messages, model, effort, fast mode, and the permission mode reach a live run, and the process engine spawns the runner binary through uagent. Both keep uagent's guards, session lock, and run records, apply the command rules, and write the same session files, so a session can move between them; one capability table says what the process engine does not run, and the session, `uah doctor`, and `/status` report it from there.
<!-- /memoria:export -->

The choice comes from `--engine`, `UAH_ENGINE`, or `engine` in the [configuration](../../docs/configuration.md#model-and-engine); `internal/app/setup.go` builds the engine. The [harness design](../../docs/design/harness.md#two-engines) records why there are two.

1. [The interface](#the-interface)
2. [What each engine supports](#what-each-engine-supports)
3. [Where each behavior lives](#where-each-behavior-lives)
4. [The process engine](#the-process-engine)
5. [The embedded engine](#the-embedded-engine)
6. [Remote jobs](#remote-jobs)
7. [Extending an engine](#extending-an-engine)
8. [Tests](#tests)
<!-- /memoria:section -->

<!-- memoria:section id="interface" files="engine.go capabilities.go events.go subagents.go patch.go" -->
## The interface

`engine.Engine` has three methods: `Name`, `Capabilities`, and `Start(ctx, request, options, sink) (Run, error)`. The sink receives `RunStarted` first and `RunFinished` last, from one goroutine at a time. A `Run` takes messages and settings while it is live (`Send`, `SetEffort`, `SetModel`, `SetServiceTier`, `SetMode`, `Compact`, `Clear`), stops (`Interrupt`, `Kill`), and ends (`Wait`). A method the engine cannot serve returns `ErrUnsupported`, and the session then applies the change from the next run.

`Capabilities` says what reaches a live run (`LiveInput`, `LiveEffort`, `LiveModel`, `ServiceTier`, `LiveMode`) and which features the engine runs at all (`Compaction`, `Rules`, `Approvals`, `ToolHooks`, `MCP`, `Subagents`, `ApplyPatch`, `CodexSkills`, `ContextUsage`, `Images`, `Reconnect`). The session, the TUI, and `uah doctor` read them; none of them checks the engine's name. [What each engine supports](#what-each-engine-supports) turns them into the capability table.

A model request is sent up to `core.Request.MaxAttempts` times, which the session fills from its settings (`request_max_attempts`, default `engine.DefaultMaxAttempts`, 10); both engines pass it to the runner's client, whose backoff waits 2 s, doubling to 30 s, between attempts (v0.1.1).

`engine.Options` carries what `core.Request` does not:

| Field | Meaning |
| --- | --- |
| `ServiceTier` | `""` or `"priority"` |
| `Mode` | The permission mode (`approval.Mode`): the sandbox commands run in, and whether the auto-reviewer decides alone. `""` is the engine's configured sandbox. A child gets its parent's through `AgentParent.Mode`, read when it spawns |
| `Compact`, `CompactFocus` | Compact before the run's first model request (a `/compact` sent while idle), the summary focused on `CompactFocus` when set (`/compact <focus>`) |
| `Clear` | Drop the context before the run's first model request (a `/clear` sent while idle) |
| `Ask` | How the run asks the user to approve an action. Nil means no one can answer, as in `uah run` |
| `Notify` | Adds an engine event to the session's stream, also after the run ended, such as a subagent's progress |
| `Inject` | Gives the agent a message without a turn of its own (`Session.Inject`): a subagent's `<subagent_notification>` to its parent, through `AgentParent.Inject` |

Optional interfaces are the seams the session probes with a type assertion:

| Interface | Used for | Implemented by |
| --- | --- | --- |
| `MCPLister` | `/mcp`: each MCP server's state and tools | embedded |
| `ContextReporter` | `/context`: the breakdown of the session's last model request | embedded |
| `Forgetter` | The session calls `Forget` when it closes, so per-session state (the auto-review transcript, the last request for `/context`, a pending fork, a cache key) does not outlive it | embedded |
| `io.Closer` | The session closes the engine with itself, stopping MCP servers and subagents | embedded |
| `Subagents` | The agent tools: `Attach` returns the tools to offer a run, `ToolNames` every name it answers, `Call` runs one, `Interrupt` stops a parent's children. The engine knows no tool name, schema, or result; [internal/agents](../agents/README.md) implements it | `internal/agents` |
| `Forker` | `Fork` copies a parent's history into a new child session for `spawn_agent`'s `fork_context`; `SetCacheKey` gives a session another prompt cache key (every subagent uses its root session's) | embedded |

The engine's own events join the run's stream: `CompactionStarted`, `Compacted`, `AutoReviewed`, `AgentUpdated`, `AgentActivity` (a child's tool events, for the parent's view), `PatchApplied` (the diff of an applied `apply_patch` call, `patch.go`), and `Reconnecting` and `ReconnectEnded` (a model request's retries, below). The embedded engine's `Subagents()` returns its `Subagents`, so a session can follow one child's whole stream (`session.WatchAgent`).
<!-- /memoria:section -->

<!-- memoria:section id="support" files="capabilities.go process/process.go embedded/engine.go" -->
## What each engine supports

`engine.Table` (`capabilities.go`) is the capability table: one row per feature that some engine may not run, the capability it needs, and what happens without it. It is the only place that says what the process engine cannot do:

- **At session start.** `internal/app` lists the features the configuration uses (`usedFeatures` in `internal/app/features.go`: MCP servers, PreToolUse, PermissionRequest, and PreCompact hooks, command rules, `prompt` rules, Auto mode, `[agents] enabled`, compaction keys, and skills in Codex's folders) as `session.Options.Uses`. `session.Open` shows one notice for each of them the engine does not run, such as `MCP servers: not supported by the process engine (they do not start); use the embedded engine`.
- **In `uah doctor`.** The `engine` check names what the engine lacks, and warns with the same notices when the configuration uses any of it.
- **In the TUI.** `/status` adds `the process engine runs without: …`.

Features that are on by default, such as live input or subagents, get no notice; `/status` and `uah doctor` list them.

| Feature | Capability | embedded | process |
| --- | --- | --- | --- |
| A message sent while the agent works | `LiveInput` | Reaches the agent before its next model request | Queues; ctrl+enter restarts the run with the queue |
| `/model`, `/effort`, the permission mode (shift+tab) | `LiveEffort`, `LiveModel`, `LiveMode` | From the next model request or command | From the next run: each mode's sandbox has its own `SHELL` |
| `/fast` (priority processing) | `ServiceTier` | openai and openai-codex | No; `--fast` is refused |
| Compaction, `/compact`, `/clear`, PreCompact hooks | `Compaction` | Yes | No (`session.ErrNoCompaction`) |
| Command rules: `allow` and `forbidden` | `Rules` | In the Bash tool | In the shell gate (below) |
| `prompt` rules, escalation, auto-review, Auto mode, PermissionRequest hooks | `Approvals` | Yes | A `prompt` rule refuses the command with the headless reason; no escalation; Auto mode is the workspace sandbox |
| PreToolUse hooks | `ToolHooks` | Yes | No |
| MCP servers | `MCP` | Yes | No |
| Subagents | `Subagents` | Yes | No |
| Codex's `apply_patch` and its diffs | `ApplyPatch` | On openai and openai-codex models | No |
| Codex skills (`.agents/skills`, `~/.config/uagent/skills`, `$CODEX_HOME/skills`) | `CodexSkills` | Yes | Only the runner's `.harness/skills` |
| `/context` | `ContextUsage` | Yes | No |
| A lost connection to the model | `Reconnect` | Retried; each retry is reported (`Reconnecting`) and shown in the TUI, and a run that loses every attempt says it gave up after N attempts | Retried the same way by the runner, but nothing shows the attempts, and a failed run shows the runner's error |
| Images pasted into the prompt | `Images` | Sent to the model with the message | The TUI shows the notice and keeps a pasted path as text; the model can still open an image file with ViewImage |
| An interrupt | | A hard stop through the runner's inbox; the session file records the stopped tools | uagent interrupts the runner process |
| `unreal-agent-runner` binary | | Not needed | Required |

Both engines read the host prompt with the instruction files from the request, keep uagent's guards (timeout, disk limit, session lock), and write run records. The embedded engine never loads the workspace `.env`.
<!-- /memoria:section -->

<!-- memoria:section id="behaviors" files="capabilities.go process/shell.go process/shellgate/gate.go" -->
## Where each behavior lives

This audit (item 33 of the ledger) lists each behavior, the code that does it, and whether both engines share it. "Shared" means one piece of code serves both engines.

| Behavior | Where | Shared? |
| --- | --- | --- |
| Instructions (AGENTS.md and the host prompt) | `internal/app` loads them into `Settings.SystemPrompt`; the session sends it with each request | Shared |
| Skills | embedded: `embedded/skills.go`, through the runner's `SkillUse`; process: the runner finds `.harness/skills` itself | Engine-specific; the runner cannot load other folders |
| SessionStart, UserPromptSubmit, PostToolUse, Stop, SessionEnd hooks | `internal/session/hooks.go` | Shared |
| PreToolUse hooks | `embedded/pretooluse.go`, around the tool registry | Embedded only |
| PermissionRequest hooks | `internal/session/approvals.go`, in the ask the approver calls | Embedded only: a process run never asks |
| PreCompact hooks | `internal/app/setup.go` (`preCompactHook`), called by `embedded/compact.go` | Embedded only |
| SubagentStop hooks | `internal/agents` | Embedded only |
| Permission mode to sandbox | `approval.Mode.Sandbox()`; the session sends the mode as `Options.Mode` | Shared; embedded switches the shell per command (`embedded/mode.go`), process per run (`process.NewSandboxed`) |
| The sandbox | `internal/sandbox`: `Wrap` and `Shell` | Shared; embedded picks a shell per command, process gives the runner one `$SHELL` per mode (`process.Shells`) |
| Rules: `allow`, `forbidden`, `prompt` | `approval.Approver.Decide` | Shared: embedded calls it in the Bash tool (`embedded/sandboxtool.go`), process in the shell gate, with no one to ask |
| Escalation, approvals, auto-review, Auto mode | The approver, `embedded/autoreview.go`, and the session's ask | Embedded only |
| MCP servers | `internal/mcp`, `embedded/mcptool.go` | Embedded only |
| Subagents | `internal/agents`, `embedded/agenttool.go` | Embedded only |
| Compaction and `/clear` | `embedded/compact.go` and `internal/compaction`; the session keeps the pending request (`internal/session/compact.go`) | Embedded only |
| `/context` | `embedded/context.go` (`ContextReporter`) | Embedded only |
| `apply_patch` | `embedded/patchtool.go` and `internal/patch` | Embedded only |
| Session settings, saved and restored | `internal/session/saved.go` and `sidecar.go` | Shared |
| Model catalog | `internal/models`: the TUI's `/model` list (both), the subagents' model check, `apply_patch` per model, and the context window for compaction (embedded) | Shared where both use it |
| Crash cleanup | uagent's `harness.Start` kills the tools a crashed run left behind, before the next run of the session (uagent v0.4.2) | Shared |
| Notices for what an engine cannot do | `engine.Table`, shown by `session.Open` from `Options.Uses` | Shared |

What moved to one place:

- **The gaps.** The notices were `if`s in `internal/app/setup.go` (PreToolUse hooks, MCP servers), and `uah doctor` checked the engine's name for MCP. They are now rows of `engine.Table`, read through `Capabilities`.
- **The rules.** They applied only on the embedded engine. The process engine now applies them through the same `approval.Approver`, in the shell gate.
- **The process engine's shells.** The closure in `internal/app/setup.go` that built each mode's sandboxing shell is `process.Shells`, which the app and the tests share.

Already shared, and confirmed: the session-level hooks, the instructions, the saved settings, the mode's sandbox (`approval.Mode.Sandbox`), and the crash cleanup (uagent). Not moved: PreToolUse hooks in the shell gate, because the gate has no session or run ID for the hook input and no way to report `HookRan` to the session, so it would run half a hook; approvals in the gate, because the gate cannot reach the user.
<!-- /memoria:section -->

<!-- memoria:section id="process" files="process/process.go process/shell.go process/shellgate/gate.go process/shellgate/main.go" -->
## The process engine

`process.Engine` is a thin adapter over uagent's `harness.Harness`: `Start` spawns the runner, and every live setter returns `ErrUnsupported`. The runner reads its request once, so the session queues messages until the run ends.

The runner (v0.1.1) runs every Bash command as `$SHELL -c <command>` (`harness/operation/shell.go`) and nothing else through `$SHELL`. So `process.Shells` builds the runner's `SHELL` for each sandbox mode, and that shell is where the process engine applies what the embedded engine applies in its Bash tool:

1. `sandbox.Shell` writes a script that runs the real shell inside the mode's sandbox, and another one without a sandbox (only the environment policy).
2. With command rules and a gate executable (`Inputs.Gate`, which `uah` sets to itself), `shellgate.Write` writes a third script, `exec uah __shell-gate <config> "$@"`, whose config carries the rules, the approval policy, and the two shells. `cmd/uah` runs `shellgate.Main` before anything else when its first argument is `__shell-gate`.
3. For `-c <command>`, the gate calls `approval.Approver.Decide` with no one to ask, as a headless embedded run does: a `forbidden` rule, or a `prompt` rule, prints the reason (`not run: a rule forbids this command: …`) on standard error and exits 1, so the model reads it as the command's output; an `allow` rule execs the shell without a sandbox; any other command execs the sandboxed shell. A shell a command starts itself (not `-c`) runs in the sandbox.

The rules see the same command string as on the embedded engine, so they are as strong there as here: they match the command's words, not what a script it runs does. Without rules, `$SHELL` is the sandboxing script, with no gate. That script cannot ask for more access. `process.NewSandboxed` keeps one harness per sandbox mode, built on first use, and each run uses its permission mode's (`Options.Mode`). `process.Capabilities(rules)` is the process engine's capabilities: only `Rules`, when the shells have a gate.
<!-- /memoria:section -->

<!-- memoria:section id="embedded" files="embedded/engine.go embedded/wiring.go embedded/agent.go embedded/adapter.go embedded/client.go embedded/providers.go embedded/clients.go embedded/reconnect.go codexauth/codexauth.go embedded/store.go embedded/observer.go embedded/tools.go embedded/sandboxtool.go embedded/sandboxschema.go embedded/skills.go embedded/pretooluse.go embedded/autoreview.go embedded/compact.go embedded/context.go embedded/fork.go embedded/mode.go embedded/patchtool.go embedded/images.go" -->
## The embedded engine

The embedded engine is a uagent `harness.Backend`. uagent still owns the run: the guards, the session lock, the run record, and the output stream. The backend (`wiring.go`) reproduces unreal-agent-runner v0.1.1's `Run` (`cmd/internal/agentrunner/run.go`) in the same order:

1. The provider client and the model (`client.go`, `providers.go`, a copy of the runner's provider table). `clients.go` builds each provider's Responses client as the runner's does, but over an HTTP client uah makes, so the engine can watch its retries (below). The ChatGPT credentials for openai-codex come from `codexauth`, which the model catalog (`internal/models`) shares.
2. The session store and the per-invocation log (`store.go`).
3. The tool registry (`tools.go`, below).
4. The operation manager with the remote job handlers.
5. The inbox, with the initial effort, the messages, and "stop when idle" (`agent.go`).
6. The context builder with the host prompt, the skills, and the tools.
7. The coordinator, on its own goroutine. A panic in runner code becomes an error, so it cannot take down the TUI.

Every opened resource adds a closer; a failed start closes them in reverse, and after a successful start the coordinator's goroutine closes them when it returns.

### A live run

`agent` is the `harness.Process`. Messages go into the runner's inbox as external inputs, and effort changes as `UpdateSettings` control messages. The permission mode lives in the run's `modeCell` (`mode.go`): the Bash translator reads it for each command and picks that mode's sandboxing shell (one per sandbox mode, built at the start), the switcher rewrites Bash's definition in each model request to describe that sandbox, and the ask reads it for each approval. In Auto mode the auto-reviewer decides alone, also with `approvals_reviewer = "user"`, and its "ask the user" becomes a decline with its reason. An interrupt is a `StopHard` control message: the coordinator cancels the model call and the tools, records their state, and returns. uagent cancels the context only after its grace period.

### Model adapters

The coordinator calls one `llm.Adapter`. Two adapters sit in front of the provider client:

| Adapter | File | Does |
| --- | --- | --- |
| `compactor` | `compact.go` | Decides when to compact (`/compact`, or the context in use reaching the automatic limit of `Config.Compaction`), runs the compaction as a job under the run's context with the configured summary model, effort, and prompt, and rewrites every request with the session's latest compaction. An automatic compaction that leaves the context above the limit reports `Compacted.Warning` and stops automatic compaction for the run. The rewrite, the summary call, and the log live in [internal/compaction](../compaction/README.md) |
| `switcher` | `adapter.go` | Applies the live model to each request, Bash's definition for the live permission mode, and routes to the priority client when fast mode is on, so `/model`, `/fast`, and shift+tab apply from the next request. It records each session's last request for `/context` (`context.go`) |
| `switcher.images` | `images.go` | Gives the model the images pasted into user messages. The runner's `llm.Message` holds text only (v0.1.1), so each request is rewritten: a message loses its `<uah-image …/>` tag lines, and each image follows it as a `ViewImage` call and its result with the image's data URL from `<state>/images`, the one image input the runner's Responses encoder sends. The call IDs follow the item's place, so the prompt cache still matches; a missing file becomes an error text. See the [images design](../../docs/design/images.md) |

`switcher.direct()` is the same client without the live model override, for one-shot calls that choose their own model: the auto-reviewer and the compaction summary. It adds pasted images too, so a summary sees them. The switcher also replaces the prompt cache key when the session has another (`SetCacheKey`).

### Retries

The runner's Responses client retries a failed attempt in its own loop: a lost connection, a stream cut halfway, a timeout, or a 408, 425, 429, or most 5xx statuses, after 2 s, then 4, 8, and 16 s, then every 30 s, less up to a fifth of jitter, or after the server's `Retry-After` up to 30 s (`responsesapi/stream.go`, v0.1.1). It reports nothing while it waits, and its constructors take no transport. So `reconnect.go` watches from outside:

1. `watched` wraps each client. Every request gets a tracker in its context, with the attempt limit.
2. `watchTransport`, the transport of the HTTP client `clients.go` builds, sees each attempt through that context. A failed connection, a stream that drops before it ends, or a status the client retries emits `engine.Reconnecting` with the next attempt, the limit, the runner's delay for it, and the reason. A response that starts, or the request's end, emits `ReconnectEnded`.
3. When the last attempt lost its connection, the request fails with "gave up after N attempts because the connection to the model was lost", and the run reports that error without the coordinator's wrapping.

The delay is the runner's policy for the failed attempt (`primitives.RemoteRetryPolicy.Backoff`), not the jittered wait itself, which can be up to a fifth shorter. A 429 the client does not retry, such as a usage limit, shows a retry for as long as it takes the client to return.

### Forked sessions

`Fork` (`fork.go`) copies a parent's history into a new child session for `spawn_agent`'s `fork_context`: the items before the parent's turn that made the spawn call, through the runner store's append methods (its own `Store.Fork` drops the operation snapshots of inherited tool calls in v0.1.1), with unfinished operations recorded as canceled, and the parent's compactions until then. On the child's first run, `openStore` adds the run's messages and effort to the store before the coordinator restores it, because the coordinator asks the model at once for the copied inputs; the inbox then drops the messages as seen. [internal/agents](../agents/README.md#forking) describes the behavior.

### The event stream

`lockedSink` wraps the run's sink so the engine can add its own events: one goroutine at a time, and none after `RunFinished`. Its `tap` sees every event first and feeds the auto-reviewer's transcript (`autoreview.go`), one per session ID so a subagent's reviews see only its own session: the user's messages and the latest tool calls, without their output.

### The tool registry

`wiring.tools` builds the registry in layers; the last layer is the outermost:

1. The runner's Bash and ViewImage translators. With a sandbox configured, Bash is `sandboxedBash` (`sandboxtool.go`), which asks the approver how each command runs. See [the permission pipeline](../approval/README.md).
2. `sandboxRegistry` (`sandboxschema.go`) adds `sandbox_permissions` and `justification` to Bash's schema when the model can ask for escalation.
3. Skills from the Codex skill folders (`skills.go`), through the runner's `SkillUse` tool.
4. MCP tools (`mcptool.go`), named `mcp__<server>__<tool>`.
5. Codex's `apply_patch` (`patchtool.go`), when the model's catalog entry has `apply_patch_tool_type`, and always on openai and openai-codex (`models.ApplyPatch`). The translator parses the patch and checks it against the files first, as Codex verifies a patch before asking, then applies the sandbox policy of the run's current permission mode: a write inside the writable roots (not a protected path) goes ahead; any other write, and every write in read only, goes to the approver as a Bash escalation would; full access applies everything. See [patches](../patch/README.md) and [the permission pipeline](../approval/README.md#patches).
6. The agent tools (`agenttool.go`), when `Subagents` lets the session spawn.
7. PreToolUse hooks (`pretooluse.go`), around every tool that a hook matches. A tool whose hook input differs from its arguments (`apply_patch`'s `{"command": patch}`) shapes it through `hookShaper`.

A tool's static definition is what the model is offered, so a change in a layer reaches both the model and the translator.

The observer (`observer.go`) writes each session item as the runner prints it. When an item completes an `apply_patch` job, it also emits `PatchApplied` with the diff from the job's handle; `session.Load` reads the same item from a run's events file, so a live and a reloaded transcript show the same diff.
<!-- /memoria:section -->

<!-- memoria:section id="jobs" files="embedded/mcptool.go embedded/mcpjobs.go embedded/agenttool.go embedded/agentjobs.go embedded/patchjobs.go" -->
## Remote jobs

A slow tool must not hold up the coordinator. MCP calls, the agent tools, and patches therefore run as the runner's remote jobs: the translator returns a remote job plan, and a `RemoteJobHandler` runs it on its own goroutine and reports `awaiting`, then the result, on its updates channel. The model keeps working meanwhile.

| Plan type | Handler | Runs |
| --- | --- | --- |
| `uah.mcp_call` (version 1) | `mcpJobs` | One MCP tool call through `internal/mcp` |
| `uah.agent` (version 1) | `agentJobs` | Whatever tools `engine.Subagents.Attach` returned (Codex's `spawn_agent`, `send_input`, `resume_agent`, `wait_agent`, `close_agent`), each through `Subagents.Call`. The plan keeps the model's call ID, which `fork_context` needs |
| `uah.apply_patch` (version 1) | `patchJobs` | One approved patch: it reads the files, writes the changes, and completes with Codex's summary as the result and the diff (`engine.PatchHandle`) as the handle, which the session file keeps untruncated |

A job that had already started before the run stopped fails with "interrupted" when the session resumes, instead of running twice. Cancelling a job cancels its context. The calls of one model turn run at the same time, each on its own goroutine (`TestAgents_ParallelCalls`).
<!-- /memoria:section -->

<!-- memoria:section id="extending" files="engine.go capabilities.go embedded/tools.go embedded/providers.go embedded/wiring.go" -->
## Extending an engine

| To add | Touch |
| --- | --- |
| A live setting | A `Capabilities` field in `capabilities.go` and a `Run` method in `engine.go`; `process.go` returns `ErrUnsupported`; `embedded/agent.go` delivers it; `internal/session/dispatch.go` (`onSettings`) calls it |
| A feature one engine does not run | A `Capabilities` field, a `Feature`, and a row of `engine.Table` in `capabilities.go`; `usedFeatures` in `internal/app/features.go` when a configuration key turns it on. The notice, `uah doctor`, and `/status` follow |
| A built-in tool | A registry layer in `embedded/tools.go`, wrapping the registry as the MCP and agent layers do. Use a remote job if the call can take long |
| A provider | `embedded/providers.go`, mirroring the runner's table, and its client in `embedded/clients.go`, built as the runner's client is, over `remoteHTTPClient`; set `Priority` if it accepts `service_tier = "priority"`, and add it to `TestClients_MatchTheRunner` |
| A session-level query | An optional interface in `engine.go`, implemented by the embedded engine and probed by `internal/session` |

The runner stays unchanged: uah reproduces its wiring instead of patching it, and the equivalence test below keeps the two in step.
<!-- /memoria:section -->

<!-- memoria:section id="tests" files="capabilities_test.go process/process_test.go process/gate_test.go process/shellgate/gate_test.go embedded/embedded_test.go embedded/approval_test.go embedded/compact_test.go embedded/compact_settings_test.go embedded/context_test.go embedded/mcp_test.go embedded/mcpjobs_internal_test.go embedded/sandbox_test.go embedded/mode_test.go embedded/images_test.go embedded/clients_test.go embedded/reconnect_test.go" -->
## Tests

The embedded tests, and the process tests with the real runner, run against `testing/fakellm`, a scripted Responses API, and need no tokens.

| Test | Pins |
| --- | --- |
| `capabilities_test.go` | The capability table: what a set of capabilities lacks, which used features get a notice, and its text |
| `process/process_test.go` | On uagent's fake runner: a run's events and result, every live setter returning `ErrUnsupported`, the empty capabilities, an interrupt, and one harness per permission mode's sandbox, built when a run first asks for it, whose runner gets that sandbox's shell |
| `process/gate_test.go` | The gate shell as the runner calls it (the test binary is the gate): `forbidden` and `prompt` refused with the embedded engine's reasons, `allow` outside a read-only sandbox, the rest in it; and with the real runner (skipped under `-short`), the model's forbidden command not run and its reason in the next request |
| `process/shellgate/gate_test.go` | The gate's decisions, the approval policy, calls other than `-c`, and its script |
| `TestEmbedded_MatchesTheRunner` | The real `unreal-agent-runner` (built from go.mod's version) and the embedded engine get the same script and must produce the same events and session items. `go test -short` skips it |
| `TestEmbedded_SteersALiveRun`, `TestEmbedded_ChangesSettingsLive`, `TestEmbedded_InterruptThenContinue`, `TestEmbedded_ResumesAProcessSession` | Live input, live settings, interrupts, and moving a session between engines |
| `approval_test.go`, `sandbox_test.go` | Escalation, rules, "don't ask again", headless denial, PermissionRequest hooks, auto-review, and the sandbox |
| `mode_test.go` | Permission modes: a live switch to read only makes the next write fail in the sandbox and the next request describe it; Auto mode lets the reviewer allow or decline without asking, also once its breaker opens |
| `compact_test.go`, `compact_settings_test.go`, `clear_test.go`, `context_test.go` | Manual and automatic compaction, the configured summary model, prompt, focus, token limit, and kept-message cap, the stop when compacting cannot get under the limit, `/clear` in the same session, resume after both, the PreCompact hook, and `/context` |
| `mcp_test.go`, `mcpjobs_internal_test.go` | MCP tools, crashes, interrupts, approvals, and jobs that are not repeated |
| `clients_test.go` | Each provider's client sends the same request as the runner's own client, body and headers |
| `reconnect_test.go` | A connection dropped before the answer and one cut halfway (`fakellm.Reply.Drop`, `Cut`) are retried after 2 and 4 s with a `Reconnecting` event each, and the run then finishes; with two attempts that both drop, the run fails and says it gave up. They wait for the real backoff (about 6 s), so `-short` skips them |
| `images_test.go` | A message with pasted images: the model gets the text without tags and each image as a ViewImage result with its data URL, a missing image as an error text, and later requests carry the image again |
<!-- /memoria:section -->
