<!-- memoria:section id="overview" files="engine.go events.go embedded/engine.go" -->
# The engine

An engine starts runs of uah-core, uah's runtime, for a session. uah has one: the `embedded` engine runs uah-core's packages inside uah, so messages and settings reach a live run.

<!-- memoria:export id="summary" -->
The embedded engine runs uah-core's packages inside uah, so messages, model, effort, fast mode, and the permission mode reach a live run. It keeps uagent's guards, session lock, and run records, applies the command rules, and writes the runner's own session files.
<!-- /memoria:export -->

`internal/app/setup.go` builds the engine for every session. The [harness design](../../docs/design/harness.md) records how it came about.

1. [The interface](#the-interface)
2. [What varies by provider and model](#what-varies-by-provider-and-model)
3. [Where each behavior lives](#where-each-behavior-lives)
4. [The embedded engine](#the-embedded-engine)
5. [The ChatGPT login](#the-chatgpt-login)
6. [Remote jobs](#remote-jobs)
7. [Extending the engine](#extending-the-engine)
8. [Adaptive effort](#adaptive-effort)
9. [Tests](#tests)
<!-- /memoria:section -->

<!-- memoria:section id="interface" files="engine.go events.go subagents.go patch.go tooloutput.go embedded/scope.go" -->
## The interface

`engine.Engine` has three methods: `Name`, `Priority`, and `Start(ctx, request, options, sink) (Run, error)`. The sink receives `RunStarted` first and `RunFinished` last, from one goroutine at a time. A `Run` takes messages and settings while it is live (`Send`, `SetEffort`, `SetModel`, `SetServiceTier`, `SetAdaptiveEffort`, `SetMode`, `Compact`, `Clear`), stops (`Interrupt`, `Kill`), and ends (`Wait`). A live change fails when the run can no longer take it, such as when it has just stopped, and the session then applies it from the next run. `Priority` says whether the session's provider accepts priority processing, which `/fast` turns on ([below](#what-varies-by-provider-and-model)).

A model request is sent up to `core.Request.MaxAttempts` times, which the session fills from its settings (`request_max_attempts`, default `engine.DefaultMaxAttempts`, 10); the engine passes it to the runner's client, whose backoff waits 2 s, doubling to 30 s, between attempts (v0.1.1).

`engine.Options` carries what `core.Request` does not:

| Field | Meaning |
| --- | --- |
| `ServiceTier` | `""` or `"priority"` |
| `Mode` | The permission mode (`approval.Mode`): the sandbox commands run in, and whether the auto-reviewer decides alone. `""` is the engine's configured sandbox. A child gets its parent's through `AgentParent.Mode`, read when it spawns |
| `Compact`, `CompactFocus` | Compact before the run's first model request (a `/compact` sent while idle), the summary focused on `CompactFocus` when set (`/compact <focus>`) |
| `Clear` | Drop the context before the run's first model request (a `/clear` sent while idle) |
| `Ask` | How the run asks the user to approve an action. Nil means no one can answer, as in `uah run` |
| `Notify` | Adds an engine event to the session's stream, also after the run ended, such as a subagent's progress |
| `Inject` | Gives the agent a message without a turn of its own (`Session.Inject`): a subagent's `<subagent_notification>` to its parent, through `AgentParent.Inject`; the returned withdraw takes it back while it is still held |
| `Stream` | Report the model's text as it arrives, for the run's own turn requests: the TUI and `uah run --stream` set it through `session.Options.Stream` |

Optional interfaces are the seams the session probes with a type assertion. The embedded engine implements each of them; a subagent's session gets the parent's engine without `Rewinder` and `MCPStarter`, so it cannot go back and connects no MCP server of its own (`session.ErrNoRewind`), and tests use engines without them:

| Interface | Used for | Implemented by |
| --- | --- | --- |
| `MCPLister` | `/mcp`: each MCP server's state and tools | embedded |
| `MCPStarter` | An interactive session connects the MCP servers as it opens, before any message: `StartMCP` waits until each has started or failed and reports them. A subagent's session gets the parent's engine without it | embedded |
| `ContextReporter` | `/context`: the breakdown of the session's last model request | embedded |
| `Forgetter` | The session calls `Forget` when it closes, so per-session state (the auto-review transcript, the last request for `/context`, a pending fork, a cache key) does not outlive it | embedded |
| `io.Closer` | The session closes the engine with itself, stopping MCP servers and subagents | embedded |
| `Subagents` | The agent tools: `Attach` returns the tools to offer a run, `ToolNames` every name it answers, `Call` runs one, `Interrupt` stops a parent's children. The engine knows no tool name, schema, or result; [internal/agents](../agents/README.md) implements it | `internal/agents` |
| `Rewinder` | `Rewind` cuts a session's context before an earlier message, as Codex's backtrack (`Session.Rewind`), and returns `Rewound` and the texts that went to the agent with the message, which the session holds again | embedded |
| `Forker` | `Fork` copies a parent's history into a new child session for `spawn_agent`'s `fork_context`; `SetCacheKey` gives a session another prompt cache key (every subagent uses its root session's) | embedded |
| `Scoper` | `SetScope` narrows one session from its next run (`embedded/scope.go`): the tools it is offered, actions approved in advance (a role's `tools` and `approve`), and `NeverAsk`, which declines every action that would ask, before the auto-reviewer, for `/review`'s reviewer | embedded |

The engine's own events join the run's stream: `CompactionStarted`, `Compacted` (with the compaction's `Stats`), `AutoReviewed`, `AgentUpdated`, `AgentActivity` (a child's tool events, for the parent's view), `PatchApplied` (the diff of an applied `apply_patch` call, `patch.go`), `ToolOutput` (the end of a failed command's output, or an MCP call's result or error, `tooloutput.go`; a status line without a nonzero exit code, a terminal error, or an MCP plan is not parsed), `Rewound` (the session went back to before a message), `Reconnecting` and `ReconnectEnded` (a model request's retries and waits for the network, below), `ModelProgress` (a turn request's phase and the tool call being written, below), `AutoReviewing` (the reviewer started; an `AutoReviewed` always follows, outcome `error` when it failed), `TextDelta`, `ReasoningDelta`, and `StreamReset` (the answer as it arrives, below), and `WebSearch` (a hosted web search, below). The embedded engine's `Subagents()` returns its `Subagents`, so a session can follow one child's whole stream (`session.WatchAgent`).
<!-- /memoria:section -->

<!-- memoria:section id="varies" files="engine.go embedded/engine.go embedded/providers.go" -->
## What varies by provider and model

Every feature runs on the one engine. What still varies is the provider and the model, and uah says so where it matters:

| Feature | Varies by | Where |
| --- | --- | --- |
| `/fast` and `--fast` (priority processing) | Provider: openai and openai-codex accept `service_tier = "priority"` | `Provider.Priority` in `embedded/providers.go`. `Engine.Priority` reports it for the session's provider, which `/fast`, `/config`, and a subagent role's tier read; `embedded.Priority` lets `app.Resolve` refuse `--fast` or `fast = true` for another provider before an engine exists |
| Codex's `apply_patch` and its diffs | Model: the catalog entry's `apply_patch_tool_type`, and always on openai and openai-codex | `models.ApplyPatch`, read when a run builds its tools ([below](#the-tool-registry)); other models edit files with commands |
| `model_verbosity` (`text.verbosity`) | Model: the catalog entry's `support_verbosity` and `default_verbosity` | `models.Verbosity`, which the switcher (`embedded/adapter.go`) asks once per model and run and sets on each request, turn or direct, as Codex's `ModelClient` does; a model without support gets no `text` field |
| Web search (`web_search`) | Provider: openai and openai-codex run the hosted tool | `Provider.WebSearch` in `embedded/providers.go`; other providers never get the tool ([below](#web-search)) |
| Automatic compaction | Model: its context window | `internal/compaction`'s window table, or `model_context_window` |
| Remote compaction (`remote_compaction`) | Provider: openai and openai-codex compact into an encrypted item | `Provider.RemoteCompaction` in `embedded/providers.go`; other providers use the summary ([below](#remote-compaction)) |

A run keeps uagent's disk limit and session lock and writes run records. uah sends no timeout, so uagent puts no wall-clock limit on a run or a subagent's run. The engine never loads the workspace `.env`.
<!-- /memoria:section -->

<!-- memoria:section id="behaviors" files="embedded/tools.go embedded/mode.go embedded/skills.go embedded/pretooluse.go" -->
## Where each behavior lives

This audit (items 33 and 51 of the ledger) lists each behavior and the code that does it.

| Behavior | Where |
| --- | --- |
| Instructions (AGENTS.md and the host prompt) | `internal/app` loads them into `Settings.SystemPrompt`; the session sends it with each request |
| Skills | `embedded/skills.go`, through the runner's `SkillUse`, from Codex's folders and the runner's `.harness/skills` |
| SessionStart, UserPromptSubmit, PostToolUse, Stop, SessionEnd hooks | `internal/session/hooks.go` |
| PreToolUse hooks | `embedded/pretooluse.go`, around the tool registry |
| PermissionRequest hooks | `internal/session/approvals.go`, in the ask the approver calls |
| PreCompact hooks | `internal/app/setup.go` (`preCompactHook`), called by `embedded/compact.go` |
| SubagentStart and SubagentStop hooks, and which hooks a subagent fires | `internal/agents` |
| Permission mode to sandbox | `approval.Mode.Sandbox()`; the session sends the mode as `Options.Mode`, and the engine switches the shell per command (`embedded/mode.go`) |
| The sandbox | `internal/sandbox`: `Wrap` and `Shell`; the engine picks a shell per command |
| Rules: `allow`, `forbidden`, `prompt` | `approval.Approver.Decide`, called in the Bash tool (`embedded/sandboxtool.go`) |
| Escalation, approvals, auto-review, Auto mode | The approver, `embedded/autoreview.go`, and the session's ask |
| MCP servers | `internal/mcp`, `embedded/mcptool.go` |
| Subagents | `internal/agents`, `embedded/agenttool.go` |
| Compaction and `/clear` | `embedded/compact.go`, `compactrun.go`, `compactremote.go`, `remotecompact.go`, and `internal/compaction`; the session keeps the pending request (`internal/session/compact.go`) |
| `/context` | `embedded/context.go` (`ContextReporter`) |
| Going back to an earlier message | `embedded/rewind.go` (`Rewinder`, `cutStore`), the cut log in `internal/compaction/rewind.go`, and `internal/session/rewind.go` |
| `apply_patch` | `embedded/patchtool.go` and `internal/patch` |
| Session settings, saved and restored | `internal/session/saved.go` and `sidecar.go` |
| Model catalog | `internal/models`: the TUI's `/model` list, the subagents' model check, `apply_patch` per model, and the context window for compaction |
| Crash cleanup | uagent's `harness.Start` kills the tools a crashed run left behind, before the next run of the session (uagent v0.4.2) |
<!-- /memoria:section -->

<!-- memoria:section id="embedded" files="embedded/engine.go embedded/wiring.go embedded/agent.go embedded/adapter.go embedded/client.go embedded/providers.go embedded/clients.go embedded/modelcall.go embedded/transport.go embedded/sse.go embedded/websearch.go embedded/searchlog.go codexauth/codexauth.go embedded/store.go embedded/sessionlog.go embedded/observer.go embedded/tools.go embedded/sandboxtool.go embedded/sandboxschema.go embedded/skills.go embedded/pretooluse.go embedded/autoreview.go embedded/compact.go embedded/compactrun.go embedded/compactremote.go embedded/remotecompact.go embedded/context.go embedded/fork.go embedded/mode.go embedded/patchtool.go embedded/images.go embedded/rewind.go embedded/wake.go embedded/prefetch.go" -->
## The embedded engine

The embedded engine is a uagent `harness.Backend`. uagent still owns the run: the guards, the session lock, the run record, and the output stream. The backend (`wiring.go`) reproduces unreal-agent-runner v0.1.1's `Run` (`cmd/internal/agentrunner/run.go`) in the same order:

1. The provider client and the model (`client.go`, `providers.go`, a copy of the runner's provider table). `clients.go` builds each provider's Responses client as the runner's does, but over an HTTP client uah makes, so the engine can watch its retries (below). The ChatGPT credentials for openai-codex come from `codexauth`, which the model catalog (`internal/models`) and the usage reader share: the codex client's transport sets them on each request and refreshes them ([below](#the-chatgpt-login)).
2. The session store (`store.go`).
3. The tool registry (`tools.go`, below).
4. The operation manager with the remote job handlers.
5. The inbox, with "stop when idle" (`agent.go`). The initial effort and the messages go into the session before the coordinator restores it (`openStore`), so it asks the model once with them and any input a stopped run left unread; a session with an operation still to finish gets them through the inbox instead, so the model is asked once that result is in. The items are written to the run's output before the coordinator starts. With [adaptive effort](#adaptive-effort) on, a new main session's messages start with the workspace context.
6. The context builder with the host prompt (uah's default prompt when the request has none), the skills, and the tools.
7. The coordinator, on its own goroutine. A panic in runner code becomes an error, so it cannot take down the TUI.

Every opened resource adds a closer; a failed start closes them in reverse, and after a successful start the coordinator's goroutine closes them when it returns.

The session store is the runner's own, under `<state>/sessions`, and uagent writes the run records, as when the runner binary runs. The runner's `localfile` store decodes the whole file for every page of items, and the usage seed and the coordinator's restore each page through the whole history, so the run's store serves those pages from one read until the run records an item after them (an 84 MB session started in 1.2 s with 2.2 GB allocated, now 0.27 s and 0.6 GB; on [uah-core](#uah-core), `localfile` keeps what `Resume` decoded and serves that read from it while the file is unchanged, so a run start decodes the file once). That store is a `logStore` (`sessionlog.go`), and the run writes through it, not through `localfile`; the same read builds its check of what it writes, where `localfile`'s first append decoded the file again (56 MB session: 1.17 GB allocated to 0.95 GB, first request 515 to 416 ms). It appends the lines `localfile` writes, byte for byte, but syncs the file only before a record a crash of the system must not lose (an operation state that is not terminal, a tool status with the result of an operation other than a command, a response that ends the turn), within 100 ms for the rest, and at the run's end. One sync covers every record before it. If that last sync, the file's close, or the write of the operation states held for the run's end fails, the run reports the error and ends failed (an interrupted run stays interrupted) (`TestFinish_SaveFailureFailsTheRun`). Later reads go to `localfile`, which reads the file and so sees every record written. A turn of six commands and a patch syncs 21 times instead of 51; the [state storage design](../../docs/design/state.md#syncing-the-session-file) gives the policy and why it re-runs nothing. So a session the runner binary started resumes here too: the coordinator restores the runner's history from its session file (`TestEmbedded_ResumesAProcessSession`, and `TestRunResumesAProcessSession` through `uah run --session`).

### The wake policy

Upstream's coordinator wakes the model for each finished tool call a second after the turn. A model waiting on a slow command was woken by each quick one, saw the slow one as "Tool call is still running", and filled the wait with `git status`, `ps`, and `sleep`. uah sets uah-core's `coordinator.WakePolicy` (`wake.go`) so that the model is not woken just to hear that a call is still running:

- A turn's results, immediate ones included, wait until every call the turn issued has finished, for every tool.
- A call still running after 5 minutes (the longest wait of Codex's `write_stdin` on a running command, codex-rs `main`, checked 2026-10-02) wakes the model with "Still running after 5 minutes" and the tail of its output so far (`bash.Progress`); the call goes on, and its result wakes the model when it finishes.
- An inbox input, such as a user message or the 10-minute heartbeat, ends the wait at once.

On six slow-test tasks × 3 (the agent benchmark, gpt-6.1-sol, high effort), the medians against the old wake: model time −19%, requests −25%, cost −19%, wall time unchanged; on edit tasks, unchanged. `wake_test.go` runs a quick and a slow command in one turn and checks that both results arrive in one request; uah-core's `coordinator/wake_test.go` checks the hold and the valve.

### uah-core

uah runs on [uah-core](https://github.com/viktordanov/uah-core) (`github.com/viktordanov/uah-core`), uah's own runtime: the coordinator, the durable operations, the session store, and the Responses client. uah-core began as a fork of [unreallabsai/unreal-agent](https://github.com/unreallabsai/unreal-agent) v0.2.0 (MIT License, Copyright (c) 2026 Unreal Labs), first published as [viktordanov/unreal-agent](https://github.com/viktordanov/unreal-agent) (v0.3.0 to v0.5.2), and continues from its `main` at v0.6.0 with the module renamed, the runner renamed to `uah-core-runner`, and the preamble's first line, which named Unreal Agent Harness, removed (ledger item 93). The `UAH_LLM_*` variables keep their names. Its changes since unreal-agent v0.2.0:

- **Request encoding.** The Responses client encodes each history item once, writes the encoded items into the request without re-encoding the history, and keeps the previous request's encodings, so a run's next request encodes only its new items. The request bytes are unchanged (a test compares them with the old encoder). The encodings of the last request stay in memory between requests, about the size of one request body. On the large fixture (48 MB), `turn/large` went from 2.58 GB allocated and 1.30 s to 0.86 GB and 0.77 s; see the [ledger](../../docs/ledger.md) item P5.
- **Custom tools.** `llm.ToolCustom` offers a Responses API custom tool, whose input is free text, optionally sampled from a grammar (`llm.ToolGrammar`). A `custom_tool_call` becomes an `llm.ToolCall` with `Custom` set and the raw input in `Arguments`; it goes back as a `custom_tool_call`, and the adapter sends a tool result as a `custom_tool_call_output` when its call was custom. Function tools encode as before. `apply_patch` is one.
- **Wake policy.** `coordinator.Dependencies.Wake` holds a turn's results until its calls finish, with a valve for a call that runs long ([below](#the-wake-policy)); its zero value wakes as upstream's coordinator does.
- **Resume.** `localfile.Store.Resume` keeps its decoded state with the file's identity, size, and modification time. While the file is unchanged, history pages come from it until a page reaches the end, and the first write's state comes from it, where every page and the first append decoded the file again. `load/large`'s first request went from 454 to 292 ms.

The old fork keeps the branches `pr/request-encoding`, `pr/resume-write-state`, and `pr/custom-tools`, each change alone, as proposed upstream.

### A live run

`agent` is the `harness.Process`. Messages go into the runner's inbox as external inputs, and effort changes as `UpdateSettings` control messages. The permission mode lives in the run's `modeCell` (`mode.go`): the Bash translator reads it for each command and picks that mode's sandboxing shell (one per sandbox mode, built at the start), the switcher rewrites Bash's definition in each model request to describe that sandbox, and the ask reads it for each approval. In Auto mode the auto-reviewer decides alone, also with `approvals_reviewer = "user"`, and its "ask the user" becomes a decline with its reason. In Yolo mode (`--yolo`) nothing asks: the Bash tool runs unsandboxed and sets `approval.Request.Bypass`, and the MCP gate lets every tool run; only `forbid` rules refuse. An interrupt is a `StopHard` control message: the coordinator cancels the model call and the tools, records their state, and returns. It also cancels the hooks and approvals in progress, which the coordinator would otherwise wait for before it reads the stop. uagent cancels the context only after its grace period.

### Model adapters

The coordinator calls one `llm.Adapter`. Two adapters sit in front of the provider client:

| Adapter | File | Does |
| --- | --- | --- |
| `compactor` | `compact.go`, `compactrun.go`, `compactremote.go` | Decides when to compact (`/compact`, or the context in use reaching the automatic limit of `Config.Compaction`), runs the compaction as a job under the run's context, and rewrites every request with the session's latest compaction. An automatic compaction first tries elision; the summary covers all but the last tool calls and carries the state ledger. On a provider with `Provider.RemoteCompaction` and `Compaction.Remote`, the compaction goes to the provider (below) and falls back to the summary. Each record carries its stats, and `Config.Logger` gets a line per compaction. An automatic compaction that leaves the context above the limit reports `Compacted.Warning` and stops automatic compaction for the run. The rewrite, the ledger, elision, the summary call, and the log live in [internal/compaction](../compaction/README.md) |
| `switcher` | `adapter.go` | Applies the live model to each request, Bash's definition for the live permission mode, and routes to the priority client when fast mode is on, so `/model`, `/fast`, and shift+tab apply from the next request. Effort ultra goes the same way: the runner's `llm.ReasoningEffort` stops at `max` (v0.1.1), so the runner runs at max and the ultra client sends `reasoning` itself, as a request extension. It records each session's last request for `/context` (`context.go`). With [adaptive effort](#adaptive-effort) on, its `adaptiveRouter` picks each turn request's effort |
| `switcher.images` | `images.go` | Gives the model the images pasted into user messages. The runner's `llm.Message` holds text only (v0.1.1), so each request is rewritten: a message loses its `<uah-image …/>` tag lines, and each image follows it as a `ViewImage` call and its result with the image's data URL from `<state>/images`, the one image input the runner's Responses encoder sends. The call IDs follow the item's place, so the prompt cache still matches; a missing file becomes an error text. See the [images design](../../docs/design/images.md) |

`switcher.direct()` is the same client without the live model override, for one-shot calls that choose their own model: the auto-reviewer and the compaction summary. Such a call runs at ultra when it asks for ultra (`compact_effort`), or keeps the session's effort while that is ultra. It adds pasted images too, so a summary sees them. The switcher also replaces the prompt cache key when the session has another (`SetCacheKey`).

### Retries

The runner's Responses client retries a failed attempt in its own loop: a lost connection, a stream cut halfway, a timeout, a stream that ends without its completed response, an in-band `response.failed` or `error` event, or a 408, 425, 429, or most 5xx statuses. It waits 2 s, then 4, 8, and 16 s, then every 30 s (10 s doubling to 60 s for `server_is_overloaded` and `slow_down`), less up to a fifth of jitter, or the server's `Retry-After` or "try again in" hint up to 30 s (`responsesapi/stream.go`, `retry.go`, v0.1.1). It reports nothing while it waits, and its constructors take no transport. So the engine watches from outside, with one observer per model request (`modelcall.go`):

1. The switcher gives every request, turn or direct, a `modelCall` in its context, with the attempt limit.
2. `callTransport` (`transport.go`), the transport of the HTTP client `clients.go` builds, reports each attempt to it. A failed connection, a status the client retries, a stream that drops or goes quiet, an in-band failure (reason `code: message`), or a stream that ends before the response completed emits `engine.Reconnecting` with the next attempt, the limit, the runner's delay for it (`retryDelay`, the upper bound of the jittered wait), and the reason. A 2xx answer, or the request's end, emits `ReconnectEnded`.
3. When the last attempt lost its connection, the request fails with "gave up after N attempts because the connection to the model was lost", and the run reports that error without the coordinator's wrapping.

Timeouts, which the runner then retries like any other failure: every model client uses one transport (`modelTransport`), shared by the engine's runs and subagents per header timeout (`transports`) and whose idle connections close when the engine closes, with a 2-minute `ResponseHeaderTimeout` (none for a loopback server or Ollama, which may load a model first), HTTP/2 pings (30 s, 15 s to answer, 60 s per write), and TCP keepalive probes (30 s idle, every 10 s, 3 misses). A stream that sends no `data:` line for 5 minutes (`streamIdleTimeout`, as Codex's stream idle timeout) is closed with "no data from the model for 5m0s". The runner's own limit, 30 minutes without a byte per attempt, still applies.

Waiting for the network: when a dial fails because a name does not resolve, there is no route, or the dial timed out (never for a loopback server or a refused connection), the transport waits inside the same attempt, 5 s doubling to 60 s ±10 %, and emits `Reconnecting` with `Offline` and "waiting for network: …", without using up an attempt. The runner's 30-minute limit bounds the wait per attempt. A name that does not resolve is waited for 30 s in all (`notFoundWait`); then the request stops its own context with "no such host: <host>", so the runner does not retry it, and the run fails with that error.

A 429 the client does not retry, such as a usage limit, shows a retry for as long as it takes the client to return. For an HTTP error status the delay comes from `Retry-After` only; the body is not read.

Diagnostics: each attempt writes one JSON line to the run's `stderr.log` (`Launch.Stderr`) when its outcome is known, a retry when the next attempt starts: `{"diag":"model_attempt","kind":"turn|direct","attempt","max","sent_bytes","connect_ms","first_byte_ms","status","events","recv_bytes","longest_gap_ms","tool","network_wait_ms","result":"ok|retry|failed|canceled","reason","delay_ms"}`.

### Streaming

The runner's client reads the SSE stream and drops its deltas, and nothing in the runner reports partial output (v0.1.1). Each 2xx attempt's body is an `attemptBody`: the runner reads the same bytes a line at a time, after one scanner (`sse.go`, `modelCall.line`) has read the line. It decodes only the events it needs: with `Options.Stream`, `response.output_text.delta` and `response.reasoning_summary_text.delta` become `engine.TextDelta` and `engine.ReasoningDelta` (`Final` from the message's `phase` in `response.output_item.added`); the terminal events and `error` decide the attempt's outcome; a finished item is decoded only when it is a web search or a tool call. The scanner only appends to a queue; a pump goroutine emits the events, merged when they pile up, so a slow consumer never holds up the runner's read. Before the request returns, the queue is drained, so every delta precedes the runner's final `AssistantMessage` and `ReasoningSummary`, which stay authoritative.

Progress: a turn request also reports `engine.ModelProgress`: the effort it went at (`Effort`, which [adaptive effort](#adaptive-effort) may lower; the TUI's footer shows it), its phase from `httptrace` (connecting, sending with the bytes sent, waiting, streaming, done), and the tool call the model is writing, from `response.output_item.added` and the argument deltas (`response.function_call_arguments.delta`, `response.custom_tool_call_input.delta`), with its size and, for `apply_patch`, the file its last header names (`patch.LastFile`). Progress goes out at most every 200 ms while a call is written, else once a second while data flows, and a queued one replaces the one before. Direct calls report no progress.

A new attempt after text streamed, or a request that fails or is canceled after it, sends `engine.StreamReset`: that text is void. The coordinator does not wait for a request it cancels, so the run's goroutine waits, up to 5 s, for the switcher's requests in flight to end (`switcher.settle`): their events, a stopped answer's `StreamReset` among them, come before `RunFinished`. `switcher.direct()` streams no text, so compaction summaries and the auto-reviewer never stream, and a subagent's session never asks. The [streaming design](../../docs/design/streaming.md) has the research and the reasons.

### Web search

With `Config.WebSearch` (`web_search = "live"`, the default), a run on a provider with `Provider.WebSearch` (openai, openai-codex) offers the hosted tool: `hostedTools` (`websearch.go`) adds `llm.Tool{Type: ToolHosted, Name: "web_search"}` to the context builder after the registry's tools, and the runner sends it as `{"type":"web_search"}`. Subagents' runs get it too, unless the session's scope (`engine.Scope.Tools`) leaves `web_search` out, as `/review`'s reviewer's does: then the run offers no hosted tool and opens no search log, so nothing is inserted into its requests. The runner drops a response's `web_search_call` items (v0.1.1), so every turn request of a run with events reports them even without `Options.Stream`: the scanner decodes the `web_search_call` lines of `response.output_item.added` and `.done` into `engine.WebSearch` (started, then done with the action, query, URL, and pattern). The runner leaves the items out of the history, so the observer records each finished search with the ID of the output item it came before (`searchlog.go`, `sessions/<id>.websearch.jsonl`), and the transport (`rewriteBody`) inserts it before that item in every later turn request whose input has it, splicing bytes into the runner's body and changing nothing else; while the log is empty, the body is not read at all. Compaction summaries and auto-review calls are direct calls and get none; a compaction, `/clear`, or a rewind removes the anchor and so the search; a fork copies the parent's log. The [web search design](../../docs/design/web-search.md) has the research, the probes, and the rationale.

### Remote compaction

On openai and openai-codex (`Provider.RemoteCompaction`), with `remote_compaction` on, a compaction is Codex's remote one (rust-v0.159.1): `compactremote.go` sends the turn's model, tools, and history as the model sees it through the switcher, with a `remoteCall` in the request's context. The transport (`rewriteBody`, `remotecompact.go`) appends `{"type":"compaction_trigger"}` to the body's `input`, and the scanner keeps the answer's `compaction` item (forgotten when another attempt starts) and gives the runner each line with an assistant message in the item's place, since the runner's parser rejects the item type (v0.1.1). The record keeps the item; the compactor puts it in every later request's context, and the transport replaces the record's placeholder message in the body with it, byte for byte. The switcher records no `/context` request for the call. A failed call, an answer without an item, and a `/compact` with focus instructions fall back to the local summary. The [compaction design](../../docs/design/compaction.md#remote-compaction) has the probes.

### Forked sessions

`Fork` (`fork.go`) copies a parent's history into a new child session for `spawn_agent`'s `fork_context`: the items before the parent's turn that made the spawn call, written in one write as the runner store's append methods write them (`TestLogStore_WritesAsLocalfile` pins the encoding, so the child is not read back; 56 MB parent: 3.96 to 3.52 GB allocated) (its own `Store.Fork` drops the operation snapshots of inherited tool calls; one append per item synced the file each time: 5.5 s for an 84 MB parent, now 0.4 s), as history only: the runner's store resumes an operation from the state its first tool-call status recorded, kept current by operation records the copy leaves out, so each copied operation gets an operation record with the last status the parent's items show (without its state) and unfinished ones are recorded as canceled (without that, the child's first run ran every command and patch of the parent again); and the parent's compactions until then. On the child's first run, `openStore` adds the run's messages and effort to the store before the coordinator restores it, as on every run without an unfinished operation, because the coordinator asks the model at once for the copied inputs. [internal/agents](../agents/README.md#forking) describes the behavior.

### Going back to an earlier message

`Rewind` (`rewind.go`) reads the session's runner items, finds the message's inbox input by its ID, and cuts from it, or from the first of the inputs just before it, which reached the agent with it; their texts go back to the session to send again. It refuses a message that arrived while a tool call had no status, since the coordinator would run the call again. The cut is a `compaction.Rewind` in `sessions/<id>.rewind.jsonl`: the first and last runner item sequences, the message, and the context the last response before it reported. Every run then opens the store through `cutStore`, whose `Items` leaves the cut items out, so the coordinator restores the history without them; its `AppendTurn` chains a new turn to the file's latest turn, which the runner's store checks. `Fork` and the usage seed read the same filtered items. The compactor settles once per run which saved compaction applies: the newest that matches, skipping records made before a rewind that no longer match. The last request kept for `/context` is trimmed to before the message. See the [rewind design](../../docs/design/rewind.md).

### The event stream

`lockedSink` wraps the run's sink so the engine can add its own events: one goroutine at a time, and none after `RunFinished`. Its `tap` sees every event first and feeds the auto-reviewer's transcript (`autoreview.go`), one per session ID so a subagent's reviews see only its own session: the user's messages and the latest tool calls, without their output.

### The tool registry

`wiring.tools` builds the registry in layers; the last layer is the outermost:

1. The runner's Bash and ViewImage translators. With a sandbox configured, Bash is `sandboxedBash` (`sandboxtool.go`), which asks the approver how each command runs. See [the permission pipeline](../approval/README.md).
2. `sandboxRegistry` (`sandboxschema.go`) adds `sandbox_permissions` and `justification` to Bash's schema when the model can ask for escalation, and a note on the current mode's sandbox to its description: what commands may write, and whether they have network. Without network the note adds that a command needing it, localhost included, fails in the sandbox, so the model asks for `require_escalated` on the first try instead of after a failed run, and independent escalations in one response get their approvals [at once](#approvals-for-parallel-calls) ([agent tuning](../../docs/design/agent-tuning.md#network-commands-escalated-up-front)).
3. Skills from the Codex skill folders (`skills.go`), through the runner's `SkillUse` tool.
4. MCP tools (`mcptool.go`), named `mcp__<server>__<tool>`.
5. Codex's `apply_patch` (`patchtool.go`), when the model's catalog entry has `apply_patch_tool_type`, and always on openai and openai-codex (`models.ApplyPatch`). It is a custom tool, as Codex offers it: Codex's description and Lark grammar (`patch.Description`, `patch.Grammar`), and the raw patch as its input. A session recorded when it was a function tool keeps its calls as function calls with the patch in `input`: they go back as recorded, as Codex sends its history, and the Responses API takes them although no function tool is declared; every reader takes both (`patch.ParseArgs`). The translator parses the patch and checks it against the files first, as Codex verifies a patch before asking, then applies the sandbox policy of the run's current permission mode: a write inside the writable roots (not a protected path) goes ahead; any other write, and every write in read only, goes to the approver as a Bash escalation would; full access applies everything. See [patches](../patch/README.md) and [the permission pipeline](../approval/README.md#patches).
6. The agent tools (`agenttool.go`), when `Subagents` lets the session spawn.
7. PreToolUse hooks (`pretooluse.go`), around every tool that a hook matches. A tool whose hook input differs from its arguments (`apply_patch`'s `{"command": patch}`) shapes it through `hookShaper`. A hook's "allow" marks the call's context (`hookAllowed`): Bash and `apply_patch` then set `approval.Request.Approved`, and the MCP gate runs the tool without asking.

A tool's static definition is what the model is offered, so a change in a layer reaches both the model and the translator.

### Approvals for parallel calls

The coordinator translates a response's calls one at a time on its event loop, and a translator must not do I/O there. A call's PreToolUse hooks and its approval do: the auto-reviewer's model call (3 to 5 s), PermissionRequest hooks, or a wait for the user. So the run decides them off the loop (`prefetch.go`):

1. A translator that waits for a decision is a `gatedTranslator`: `decide(ctx, call)` does the waiting and returns the rest of `Translate`, which only submits the call's operation or returns its error. `sandboxedBash`, `patchTranslator`, `mcpTranslator`, and `hookedTranslator` are gated; a hooked tool runs its hooks, then the inner tool's `decide` with the arguments the hooks left.
2. The `prefetcher` observes the session store. When the coordinator stores a model response, just before it translates the response's calls, the prefetcher records the calls in the auto-reviewer's transcript and starts a decision for each gated call, each on its own goroutine, under the run's approval context.
3. The registry the coordinator runs wraps each gated translator: `Translate` takes its call's decision, waits for it if it is not done yet, and runs the rest. A call with no decision started, such as one a resumed session left untranslated, or one whose arguments differ from the stored call's, is decided in `Translate` as before.
4. The next stored response cancels the decisions no `Translate` took. An interrupt cancels the approval context (`agent.stopApprovals`): the reviews, hooks, and prompts in progress end at once, their calls are declined, and the coordinator reads the stop. Before, an interrupt waited for every review in turn.

Each call still gets one decision with the same inputs: the same hooks, the same request to the approver, and the same transcript, which now has all of the response's calls before the first review starts. Results stay per call, and the coordinator dispatches the operations once every call is translated, as before. So N escalations in one response take about as long as the slowest review rather than the sum: 4 parallel curl escalations waited 3.5 s instead of 11.9 s ([agent tuning](../../docs/design/agent-tuning.md#parallel-approvals)). Several prompts can be open at once; see [the approver](../approval/README.md#the-approver) for how "don't ask again" settles the others.

The observer (`observer.go`) writes each session item as the runner prints it. When an item completes an `apply_patch` job, it also emits `PatchApplied` with the diff from the job's handle, and when it finishes a shell command that failed or an MCP call, `ToolOutput` with the last 4 KB of the command's stderr (its stdout when stderr is empty) or the first 4 KB of the MCP result, or its error, each once per call; the runner's `ToolFinished` carries only the exit code or the status. `session.Load` reads the same items from a run's events file, so a live and a reloaded transcript show the same diff and the same error lines.
<!-- /memoria:section -->

<!-- memoria:section id="auth" files="codexauth/codexauth.go codexauth/login.go codexauth/refresh.go codexauth/file.go codexauth/transport.go embedded/clients.go embedded/engine.go" -->
## The ChatGPT login

`codexauth` serves the openai-codex credentials: `OPENAI_CODEX_ACCESS_TOKEN`, which uah never refreshes, or Codex's auth file (`OPENAI_CODEX_AUTH_FILE`, else `$CODEX_HOME/auth.json`), which it refreshes as Codex rust-v0.156.1 does. The [design record](../../docs/design/codex-auth.md) cites Codex's source for each rule.

1. **Each request reads the file** (`Login.Creds`), parsing it again only when it changed, so a refresh Codex wrote is used at once. A token that expires within 5 minutes is refreshed first; if that fails, a token that still works is sent.
2. **A 401 renews once** (`Login.Transport`, `Login.Renew`): the file's token when another writer already replaced the rejected one, else a refresh, and the request is sent again. The embedded engine's codex client, the model list, and the usage reader use it; the token is no longer a fixed header of the client.
3. **Before a run**, the engine calls `codexauth.BeforeRun`, which refreshes a token that expires within an hour, so uagent's preflight never blocks a token that can be refreshed.
4. **The refresh** is Codex's request to `https://auth.openai.com/oauth/token` (`refresh.go`). One refresh runs at a time per file in the process, and a lock file beside the auth file (`.auth.json.uah-lock`) orders uah processes; the file is read again under the lock before the request and before the write, so a refresh Codex wrote meanwhile wins. The write keeps every other field and Codex's layout, through a private temporary file renamed over the file.
5. **A refused refresh** (`invalid_grant`, a reused, expired, or revoked refresh token) is `codexauth.ErrLoginExpired`, "Your ChatGPT login expired; run `codex login`", and the model request answers 401, which the runner's client does not retry. No error contains a token.
<!-- /memoria:section -->

<!-- memoria:section id="jobs" files="embedded/mcptool.go embedded/mcpjobs.go embedded/agenttool.go embedded/agentjobs.go embedded/patchjobs.go" -->
## Remote jobs

A slow tool must not hold up the coordinator. MCP calls, the agent tools, and patches therefore run as the runner's remote jobs: the translator returns a remote job plan, and a `RemoteJobHandler` runs it on its own goroutine and reports `awaiting`, then the result, on its updates channel. The model keeps working meanwhile.

| Plan type | Handler | Runs |
| --- | --- | --- |
| `uah.mcp_call` (version 1) | `mcpJobs` | One MCP tool call through `internal/mcp` |
| `uah.agent` (version 1) | `agentJobs` | Whatever tools `engine.Subagents.Attach` returned (Codex's `spawn_agent`, `send_input`, `resume_agent`, `wait_agent`, `close_agent`), each through `Subagents.Call`. The plan keeps the model's call ID, which `fork_context` needs |
| `uah.apply_patch` (version 1) | `patchJobs` | One approved patch: it reads the files, writes the changes, and completes with Codex's summary as the result and the diff (`engine.PatchHandle`) as the handle, which the session file keeps untruncated. |

A job that had already started before the run stopped fails with "interrupted" when the session resumes, instead of running twice. Cancelling a job cancels its context. The calls of one model turn run at the same time, each on its own goroutine (`TestAgents_ParallelCalls`).
<!-- /memoria:section -->

<!-- memoria:section id="extending" files="engine.go embedded/tools.go embedded/providers.go embedded/wiring.go" -->
## Extending the engine

| To add | Touch |
| --- | --- |
| A live setting | A `Run` method in `engine.go`; `embedded/agent.go` delivers it; `internal/session/dispatch.go` (`onSettings`) calls it and falls back to the next run when it fails |
| A built-in tool | A registry layer in `embedded/tools.go`, wrapping the registry as the MCP and agent layers do. Use a remote job if the call can take long. A tool that asks for approval or waits for anything before it submits implements `gatedTranslator` (`decide`), so its wait runs off the coordinator's loop ([approvals for parallel calls](#approvals-for-parallel-calls)) |
| A provider | `embedded/providers.go`, mirroring the runner's table, and its client in `embedded/clients.go`, built as the runner's client is, over `remoteHTTPClient`; set `Priority` if it accepts `service_tier = "priority"`, `WebSearch` if it runs the hosted search, and `RemoteCompaction` if it answers Codex's compaction trigger; add it to `TestClients_MatchTheRunner` |
| A session-level query | An optional interface in `engine.go`, implemented by the embedded engine and probed by `internal/session` |

A change the runtime itself needs is made in [uah-core](#uah-core) and tagged there; uah wires uah-core's packages as `uah-core-runner` does and never patches them, and the equivalence test below keeps the two in step.
<!-- /memoria:section -->

<!-- memoria:section id="adaptive" files="embedded/adaptive.go embedded/primed.go embedded/adaptive_internal_test.go" -->
## Adaptive effort

Adaptive effort (`engine.Options.AdaptiveEffort`: `"1-step"` or `"2-steps"`; `"off"` or empty is off) makes the model think less on follow-up turns and starts a new session with the workspace's context. It is a session setting, like the effort: `session.Settings.AdaptiveEffort` goes with each run, and `Run.SetAdaptiveEffort` changes it for the live run's next model request (`/adaptive`, `/config`). A subagent starts with its parent's. It was called Lean mode while it was measured.

**Effort routing.** The switcher (`adapter.go`) asks its `adaptiveRouter` (`adaptive.go`) for each turn request's effort while the steps are not 0; a remote compaction and the direct calls (summaries, the auto-reviewer) keep theirs. A request whose input since the model's last output is only tool results goes the setting's steps below the user's effort E, never below low. The first request, a request with a user message (a steer, an injected notification, a heartbeat), and one without tool results go at E. The levels are low, medium, high, xhigh, max, and ultra above max: one step down from high is medium, two are low; from ultra (then on the plain client) one is max and two are xhigh.

A benchmark of finer rules, which lowered only the follow-ups after plain confirmations, raised the effort after a repeated failure, or kept reading-heavy sessions at E, found them slower and dearer than this one: a request whose effort differs from the one before it hits the prompt cache less, and they changed the effort more often ([Lean mode rules](../../docs/design/agent-tuning.md#lean-mode-rules), [Escalation on failure](../../docs/design/agent-tuning.md#escalation-on-failure)).

Each attempt's `model_attempt` line in the run's `stderr.log` carries `effort` and, with adaptive effort on, `effort_reason`: the steps and why, such as `1-step: tool results only` or `2-steps: user message`. The agent benchmark reads them.

**Primed first turn.** With adaptive effort on when it starts, a new session of the main agent (not a subagent, a fork, or a resumed session) gets one more user message before the user's: a `<workspace_context>` block of at most about 4 KB that `primed.go` gathers before the first request. Turning adaptive effort on later primes nothing. The block holds the files that instruction files include with an `@` line (such as `@RTK.md`; the system prompt keeps each instruction file under a `## <path>` header, which resolves a relative include), the git branch and `git status --short` (at most 20 lines), and `git ls-files` by top directory with file counts (at most 40 entries). Each git command has 2 seconds. The system prompt does not change, so the prompt cache holds.
<!-- /memoria:section -->

<!-- memoria:section id="tests" files="embedded/embedded_test.go embedded/patch_test.go embedded/patch_tool_test.go embedded/approval_test.go embedded/compact_test.go embedded/compact_settings_test.go embedded/context_test.go embedded/mcp_test.go embedded/mcpjobs_internal_test.go embedded/sandbox_test.go embedded/mode_test.go embedded/images_test.go embedded/clients_test.go embedded/reconnect_test.go embedded/transport_internal_test.go embedded/stream_test.go embedded/stream_internal_test.go embedded/rewind_test.go embedded/rewind_internal_test.go embedded/fork_internal_test.go embedded/store_internal_test.go embedded/sessionlog_test.go embedded/sessionlog_internal_test.go embedded/codexlogin_test.go codexauth/codexauth_test.go codexauth/login_test.go codexauth/file_internal_test.go embedded/websearch_test.go embedded/searchlog_internal_test.go embedded/compact_remote_test.go embedded/remotecompact_internal_test.go embedded/compact_probe_test.go embedded/remote_probe_test.go embedded/adaptive_test.go embedded/wake_internal_test.go embedded/parallel_test.go embedded/prefetch_internal_test.go" -->
## Tests

The tests run against `testing/fakellm`, a scripted Responses API, and need no tokens. The ones that compare with the real runner drive it through `harnesstest.RunnerEngine`, a test-only engine over uagent's harness that spawns a runner binary.

| Test | Pins |
| --- | --- |
| `TestEmbedded_MatchesTheRunner` | The real `uah-core-runner` (built from go.mod's version) and the embedded engine get the same script and must produce the same events and session items. `go test -short` skips it |
| `TestEmbedded_SteersALiveRun`, `TestEmbedded_ChangesSettingsLive`, `TestEmbedded_InterruptThenContinue` | Live input, live settings, and interrupts |
| `TestEmbedded_ResumesAProcessSession` | A session the real runner started resumes on the embedded engine with its history. `go test -short` skips it |
| `patch_test.go`, `patch_tool_test.go` | `apply_patch` end to end: the custom tool's definition, patches inside and outside the workspace, read only, protected paths, declines, verification, hooks and their `updatedInput`, the approver's input, the live mode; a session recorded with the function tool rewinds, resumes, and goes on with a custom call, its old call sent back as recorded |
| `adaptive_test.go`, `adaptive_internal_test.go` | Adaptive effort off, at 1 step, and at 2 steps: the primed context before the first message only, with the include, git's state, and the files, and the same system prompt; a follow-up after tool results one or two levels lower, a read as much as a confirmation, and each request's effort and its reason in the attempt's diagnostics; a change during a run that lowers the next follow-up, primes nothing, and turned off brings it back; which inputs keep the user's effort; the steps down, floored at low, with ultra |
| `approval_test.go`, `sandbox_test.go` | Escalation, rules, "don't ask again", headless denial, PermissionRequest hooks, auto-review (its start always paired with its end), and the sandbox |
| `parallel_test.go`, `prefetch_internal_test.go` | The approvals of one response's calls run at once: three auto-reviews that each wait for the others finish in about one review's time, each with its own verdict; three prompts open at once, each answer reaching its call; "don't ask again" on one prompt settling another; PreToolUse hooks that each wait for the others, one rewriting the command the user is then asked about; an interrupt ending three reviews at once. The prefetcher decides each gated call once, falls back to deciding in `Translate`, drops a stale decision, and refuses a call whose decision panicked |
| `mode_test.go` | Permission modes: a live switch to read only makes the next write fail in the sandbox and the next request describe it; Auto mode lets the reviewer allow or decline without asking, also once its breaker opens |
| `compact_test.go`, `compact_settings_test.go`, `clear_test.go`, `context_test.go` | Manual and automatic compaction, the configured summary model, prompt, focus, token limit, and kept-message cap, the stop when compacting cannot get under the limit, `/clear` in the same session, resume after both, the PreCompact hook, and `/context`; each compaction's stats in the event and the record, the state ledger after the summary, elision before an automatic summary (no summary call when the stubs free enough, the same stubs on later requests and after a resume), and the last calls kept verbatim |
| `compact_remote_test.go`, `remotecompact_internal_test.go` | With `fakellm.Reply.Compaction`: the compaction request is the turn's with Codex's trigger last, the next request and a resumed session carry the provider's item after the kept message and before the ledger, with no placeholder; an answer without an item falls back to the summary; a `/compact` focus uses the summary. The attempt body keeps the item from a ChatGPT-style stream and hides it from the runner's parser, forgets an item from an attempt that was cut, and replaces or appends input items with the body otherwise unchanged |
| `compact_probe_test.go`, `remote_probe_test.go` (build tag `probe`, by hand) | Real requests on openai-codex: the raw remote compaction request and a continuation from its item, and the same through the engine |
| `mcp_test.go`, `mcpjobs_internal_test.go` | MCP tools, crashes, interrupts, approvals, and jobs that are not repeated |
| `clients_test.go` | Each provider's client sends the same request as the runner's own client, body and headers |
| `transport_internal_test.go` | Retries shown with their reason and the runner's delay: a stream quiet past the idle limit, an in-band `response.failed`, a stream without its completed response (`fakellm.Reply.NoEnd`), a 429 with `Retry-After: 1` (1 s), and an overload (10 s); a server that takes the connection and never answers (`Freeze`) times out on the header limit and the retry answers; an unreachable network (a dialer failing with ENETUNREACH twice) waits within attempt 1 with `Offline` events, sends one request, and writes one `ok` diagnostics line; an `apply_patch` written in pieces (`ArgDeltas`) reports its file and size, then clears; both HTTP clients carry the timeouts and share the engine's transport, and a loopback server gets no header limit. These change package limits, so they do not run in parallel |
| `reconnect_test.go` | A connection dropped before the answer and one cut halfway (`fakellm.Reply.Drop`, `Cut`) are retried after 2 and 4 s with a `Reconnecting` event each, and the run then finishes; with two attempts that both drop, the run fails and says it gave up. They wait for the real backoff (about 6 s), so `-short` skips them |
| `stream_test.go`, `stream_internal_test.go` | With `fakellm.Reply.Deltas`, `Reasoning`, and `Hold`: the answer and reasoning arrive as deltas while the response is held, before the final message, which they match; no deltas without `Stream`, from a compaction summary, or from the auto-reviewer; a stream cut halfway is reset before the retry's text (skipped under `-short`), and an interrupt resets it too. The attempt body passes a stream read one byte at a time through unchanged and finds its deltas across reads, CRLF, and a 1 MiB line |
| `websearch_test.go` | With `fakellm.Reply.Searches`: the hosted tool in every request when the engine offers it on a provider with search, and in none when it is off or the provider has none; a search and an opened page reported as they start and finish, before the answer, in a session that does not stream text; the next request with both searches before the answer they preceded, as Codex sends them; a resumed session keeping them; a rewind, a compaction, and `/clear` dropping them, and no search in a compaction's summary request. `searchlog_internal_test.go`: insertion before or after an anchor with the body otherwise byte for byte the same, a request without an anchor unchanged, and a request passed through unread while no search is recorded |
| `rewind_test.go`, `rewind_internal_test.go` | Going back: the next request carries only the history before the message and the edited one, `/context` shrinks, the session file keeps the old branch, and resume and `session.Load` keep the cut; past a compaction that covered the message (the one before applies, no mismatch), after a `/clear` (the clear stays), an unknown message, a notification that went with the message and goes again, and where the cut starts or why it is refused |
| `fork_internal_test.go`, `store_internal_test.go` | A fork written in one write reads back as the old one-append-per-item replay did, on a recorded session cut at its end, at a turn, and mid tool call (its operation canceled), with no operation to resume; the snapshot store's pages and usage seed are the file's for any cursor and limit, and a recorded item sends reads to the file |
| `sessionlog_internal_test.go`, `sessionlog_test.go` | A recorded session written through `localfile` and through `logStore` at the same times makes the same file, byte for byte, with a sync only where the policy needs one; records `localfile` could not read back are refused; a run's session file cut after each of its syncs, as a crash of the system leaves it, resumes, never runs the command again, gives the model the whole output once `read_out` was synced, and leaves no process group for the harness to kill |
| `codexlogin_test.go` | The ChatGPT login against a fake token endpoint: a 401 from `fakellm` renews the token and the request is sent again; an expired token is refreshed before the run; a refused refresh says to run `codex login` and is not retried |
| `codexauth/login_test.go`, `codexauth/file_internal_test.go`, `codexauth/codexauth_test.go` | Refreshing before expiry with Codex's request, the file written back in Codex's layout with unknown fields, mode 0600, and a rename; a 401 retried once; sixteen concurrent renewals making one request; a token Codex wrote before or during the refresh used instead; refusals, their message, and no second attempt; the environment's token never refreshed; `BeforeRun`; the lock file; and loading and checking the credentials |
| `images_test.go` | A message with pasted images: the model gets the text without tags and each image as a ViewImage result with its data URL, a missing image as an error text, and later requests carry the image again |
<!-- /memoria:section -->
