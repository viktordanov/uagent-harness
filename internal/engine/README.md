<!-- memoria:section id="overview" files="engine.go events.go process/process.go embedded/engine.go" -->
# Engines

An engine starts runs of unreal-agent-runner for a session. The `process` engine spawns the runner binary through uagent; the `embedded` engine runs the runner's own packages inside uah, so messages and settings reach a live run.

<!-- memoria:export id="summary" -->
An engine starts runs of unreal-agent-runner for a session: the embedded engine (the default) runs the runner's packages inside uah, so messages, model, effort, and fast mode reach a live run, and the process engine spawns the runner binary through uagent. Both keep uagent's guards, session lock, and run records, and write the same session files, so a session can move between them.
<!-- /memoria:export -->

The choice comes from `--engine`, `UAH_ENGINE`, or `engine` in the [configuration](../../docs/configuration.md#model-and-engine); `internal/app/setup.go` builds the engine. The [harness design](../../docs/design/harness.md#two-engines) records why there are two.

1. [The interface](#the-interface)
2. [What each engine supports](#what-each-engine-supports)
3. [The process engine](#the-process-engine)
4. [The embedded engine](#the-embedded-engine)
5. [Remote jobs](#remote-jobs)
6. [Extending an engine](#extending-an-engine)
7. [Tests](#tests)
<!-- /memoria:section -->

<!-- memoria:section id="interface" files="engine.go events.go subagents.go" -->
## The interface

`engine.Engine` has three methods: `Name`, `Capabilities`, and `Start(ctx, request, options, sink) (Run, error)`. The sink receives `RunStarted` first and `RunFinished` last, from one goroutine at a time. A `Run` takes messages and settings while it is live (`Send`, `SetEffort`, `SetModel`, `SetServiceTier`, `Compact`, `Clear`), stops (`Interrupt`, `Kill`), and ends (`Wait`). A method the engine cannot serve returns `ErrUnsupported`, and the session then applies the change from the next run.

`Capabilities` says what reaches a live run: `LiveInput`, `LiveEffort`, `LiveModel`, `ServiceTier`, and `Compaction`. The session and the TUI read them; they never check the engine's name.

`engine.Options` carries what `core.Request` does not:

| Field | Meaning |
| --- | --- |
| `ServiceTier` | `""` or `"priority"` |
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
| `io.Closer` | The session closes the engine with itself, stopping MCP servers and subagents | embedded |
| `Subagents` | The agent tools: `Attach` returns the tools to offer a run, `ToolNames` every name it answers, `Call` runs one, `Interrupt` stops a parent's children. The engine knows no tool name, schema, or result; [internal/agents](../agents/README.md) implements it | `internal/agents` |
| `Forker` | `Fork` copies a parent's history into a new child session for `spawn_agent`'s `fork_context`; `SetCacheKey` gives a session another prompt cache key (every subagent uses its root session's) | embedded |

The engine's own events join the run's stream: `CompactionStarted`, `Compacted`, `AutoReviewed`, `AgentUpdated`, and `AgentActivity` (a child's tool events, for the parent's view). The embedded engine's `Subagents()` returns its `Subagents`, so a session can follow one child's whole stream (`session.WatchAgent`).
<!-- /memoria:section -->

<!-- memoria:section id="support" files="engine.go process/process.go embedded/engine.go embedded/tools.go" -->
## What each engine supports

| Feature | embedded | process |
| --- | --- | --- |
| A message sent while the agent works | Reaches the agent before its next model request | Queues; ctrl+enter restarts the run with the queue |
| `/model`, `/effort` | From the next model request | From the next run |
| `/fast` (priority processing) | openai and openai-codex | No |
| An interrupt | A hard stop through the runner's inbox; the session file records the stopped tools | uagent interrupts the runner process |
| Sandbox | Per command, with escalation and approvals | Every command, through a sandboxing `SHELL`; no escalation |
| Rules, auto-review, approval prompts | Yes | No |
| PreToolUse and PermissionRequest hooks | Yes | No |
| Compaction and `/context` | Yes | No |
| MCP servers | Yes | No; uah says so when some are configured |
| Subagents | Yes | No |
| Codex skills (`.agents/skills`, `~/.config/uagent/skills`, `$CODEX_HOME/skills`) | Yes | Only the runner's `.harness/skills` |
| `unreal-agent-runner` binary | Not needed | Required |

Both engines read the host prompt with the instruction files from the request, keep uagent's guards (timeout, disk limit, session lock), and write run records. The embedded engine never loads the workspace `.env`.
<!-- /memoria:section -->

<!-- memoria:section id="process" files="process/process.go" -->
## The process engine

`process.Engine` is a thin adapter over uagent's `harness.Harness`: `Start` spawns the runner, and every live setter returns `ErrUnsupported`. The runner reads its request once, so the session queues messages until the run ends.

The runner runs each command with `$SHELL`. `internal/app/setup.go` points `SHELL` at a script from `sandbox.Shell` that runs the real shell inside the sandbox, so commands are sandboxed without changing the runner. That script cannot ask for more access.
<!-- /memoria:section -->

<!-- memoria:section id="embedded" files="embedded/engine.go embedded/wiring.go embedded/agent.go embedded/adapter.go embedded/client.go embedded/providers.go codexauth/codexauth.go embedded/store.go embedded/observer.go embedded/tools.go embedded/sandboxtool.go embedded/sandboxschema.go embedded/skills.go embedded/pretooluse.go embedded/autoreview.go embedded/compact.go embedded/context.go embedded/fork.go" -->
## The embedded engine

The embedded engine is a uagent `harness.Backend`. uagent still owns the run: the guards, the session lock, the run record, and the output stream. The backend (`wiring.go`) reproduces unreal-agent-runner v0.1.1's `Run` (`cmd/internal/agentrunner/run.go`) in the same order:

1. The provider client and the model (`client.go`, `providers.go`, a copy of the runner's provider table). The ChatGPT credentials for openai-codex come from `codexauth`, which the model catalog (`internal/models`) shares.
2. The session store and the per-invocation log (`store.go`).
3. The tool registry (`tools.go`, below).
4. The operation manager with the remote job handlers.
5. The inbox, with the initial effort, the messages, and "stop when idle" (`agent.go`).
6. The context builder with the host prompt, the skills, and the tools.
7. The coordinator, on its own goroutine. A panic in runner code becomes an error, so it cannot take down the TUI.

Every opened resource adds a closer; a failed start closes them in reverse, and after a successful start the coordinator's goroutine closes them when it returns.

### A live run

`agent` is the `harness.Process`. Messages go into the runner's inbox as external inputs, and effort changes as `UpdateSettings` control messages. An interrupt is a `StopHard` control message: the coordinator cancels the model call and the tools, records their state, and returns. uagent cancels the context only after its grace period.

### Model adapters

The coordinator calls one `llm.Adapter`. Two adapters sit in front of the provider client:

| Adapter | File | Does |
| --- | --- | --- |
| `compactor` | `compact.go` | Decides when to compact (`/compact`, or the context in use reaching the automatic limit of `Config.Compaction`), runs the compaction as a job under the run's context with the configured summary model, effort, and prompt, and rewrites every request with the session's latest compaction. An automatic compaction that leaves the context above the limit reports `Compacted.Warning` and stops automatic compaction for the run. The rewrite, the summary call, and the log live in [internal/compaction](../compaction/README.md) |
| `switcher` | `adapter.go` | Applies the live model to each request and routes to the priority client when fast mode is on, so `/model` and `/fast` apply from the next request. It records each session's last request for `/context` (`context.go`) |

`switcher.direct()` is the same client without the live model override, for one-shot calls that choose their own model: the auto-reviewer and the compaction summary. The switcher also replaces the prompt cache key when the session has another (`SetCacheKey`).

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
5. The agent tools (`agenttool.go`), when `Subagents` lets the session spawn.
6. PreToolUse hooks (`pretooluse.go`), around every tool that a hook matches.

A tool's static definition is what the model is offered, so a change in a layer reaches both the model and the translator.
<!-- /memoria:section -->

<!-- memoria:section id="jobs" files="embedded/mcptool.go embedded/mcpjobs.go embedded/agenttool.go embedded/agentjobs.go" -->
## Remote jobs

A slow tool must not hold up the coordinator. MCP calls and the agent tools therefore run as the runner's remote jobs: the translator returns a remote job plan, and a `RemoteJobHandler` runs it on its own goroutine and reports `awaiting`, then the result, on its updates channel. The model keeps working meanwhile.

| Plan type | Handler | Runs |
| --- | --- | --- |
| `uah.mcp_call` (version 1) | `mcpJobs` | One MCP tool call through `internal/mcp` |
| `uah.agent` (version 1) | `agentJobs` | Whatever tools `engine.Subagents.Attach` returned (Codex's `spawn_agent`, `send_input`, `resume_agent`, `wait_agent`, `close_agent`), each through `Subagents.Call`. The plan keeps the model's call ID, which `fork_context` needs |

A job that had already started before the run stopped fails with "interrupted" when the session resumes, instead of running twice. Cancelling a job cancels its context. The calls of one model turn run at the same time, each on its own goroutine (`TestAgents_ParallelCalls`).
<!-- /memoria:section -->

<!-- memoria:section id="extending" files="engine.go embedded/tools.go embedded/providers.go embedded/wiring.go" -->
## Extending an engine

| To add | Touch |
| --- | --- |
| A live setting | A `Capabilities` field and a `Run` method in `engine.go`; `process.go` returns `ErrUnsupported`; `embedded/agent.go` delivers it; `internal/session/dispatch.go` (`onSettings`) calls it |
| A built-in tool | A registry layer in `embedded/tools.go`, wrapping the registry as the MCP and agent layers do. Use a remote job if the call can take long |
| A provider | `embedded/providers.go`, mirroring the runner's table; set `Priority` if it accepts `service_tier = "priority"` |
| A session-level query | An optional interface in `engine.go`, implemented by the embedded engine and probed by `internal/session` |

The runner stays unchanged: uah reproduces its wiring instead of patching it, and the equivalence test below keeps the two in step.
<!-- /memoria:section -->

<!-- memoria:section id="tests" files="embedded/embedded_test.go embedded/approval_test.go embedded/compact_test.go embedded/compact_settings_test.go embedded/context_test.go embedded/mcp_test.go embedded/mcpjobs_internal_test.go embedded/sandbox_test.go" -->
## Tests

The embedded tests run against `testing/fakellm`, a scripted Responses API, and need no tokens.

| Test | Pins |
| --- | --- |
| `TestEmbedded_MatchesTheRunner` | The real `unreal-agent-runner` (built from go.mod's version) and the embedded engine get the same script and must produce the same events and session items. `go test -short` skips it |
| `TestEmbedded_SteersALiveRun`, `TestEmbedded_ChangesSettingsLive`, `TestEmbedded_InterruptThenContinue`, `TestEmbedded_ResumesAProcessSession` | Live input, live settings, interrupts, and moving a session between engines |
| `approval_test.go`, `sandbox_test.go` | Escalation, rules, "don't ask again", headless denial, PermissionRequest hooks, auto-review, and the sandbox |
| `compact_test.go`, `compact_settings_test.go`, `clear_test.go`, `context_test.go` | Manual and automatic compaction, the configured summary model, prompt, focus, token limit, and kept-message cap, the stop when compacting cannot get under the limit, `/clear` in the same session, resume after both, the PreCompact hook, and `/context` |
| `mcp_test.go`, `mcpjobs_internal_test.go` | MCP tools, crashes, interrupts, approvals, and jobs that are not repeated |
<!-- /memoria:section -->
