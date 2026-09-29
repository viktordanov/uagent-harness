<!-- memoria:section id="overview" files="engine.go events.go embedded/engine.go" -->
# The engine

An engine starts runs of unreal-agent-runner for a session. uah has one: the `embedded` engine runs the runner's own packages inside uah, so messages and settings reach a live run.

<!-- memoria:export id="summary" -->
The embedded engine runs unreal-agent-runner's packages inside uah, so messages, model, effort, fast mode, and the permission mode reach a live run. It keeps uagent's guards, session lock, and run records, applies the command rules, and writes the runner's own session files.
<!-- /memoria:export -->

`internal/app/setup.go` builds the engine for every session. The [harness design](../../docs/design/harness.md) records how it came about.

1. [The interface](#the-interface)
2. [What varies by provider and model](#what-varies-by-provider-and-model)
3. [Where each behavior lives](#where-each-behavior-lives)
4. [The embedded engine](#the-embedded-engine)
5. [The ChatGPT login](#the-chatgpt-login)
6. [Remote jobs](#remote-jobs)
7. [Extending the engine](#extending-the-engine)
8. [Tests](#tests)
<!-- /memoria:section -->

<!-- memoria:section id="interface" files="engine.go events.go subagents.go patch.go embedded/scope.go" -->
## The interface

`engine.Engine` has three methods: `Name`, `Priority`, and `Start(ctx, request, options, sink) (Run, error)`. The sink receives `RunStarted` first and `RunFinished` last, from one goroutine at a time. A `Run` takes messages and settings while it is live (`Send`, `SetEffort`, `SetModel`, `SetServiceTier`, `SetMode`, `Compact`, `Clear`), stops (`Interrupt`, `Kill`), and ends (`Wait`). A live change fails when the run can no longer take it, such as when it has just stopped, and the session then applies it from the next run. `Priority` says whether the session's provider accepts priority processing, which `/fast` turns on ([below](#what-varies-by-provider-and-model)).

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
| `Inject` | Gives the agent a message without a turn of its own (`Session.Inject`): a subagent's `<subagent_notification>` to its parent, through `AgentParent.Inject` |
| `Stream` | Report the model's text as it arrives, for the run's own turn requests: the TUI and `uah run --stream` set it through `session.Options.Stream` |

Optional interfaces are the seams the session probes with a type assertion. The embedded engine implements each of them; a subagent's session gets the parent's engine without `Rewinder`, so it cannot go back (`session.ErrNoRewind`), and tests use engines without them:

| Interface | Used for | Implemented by |
| --- | --- | --- |
| `MCPLister` | `/mcp`: each MCP server's state and tools | embedded |
| `ContextReporter` | `/context`: the breakdown of the session's last model request | embedded |
| `Forgetter` | The session calls `Forget` when it closes, so per-session state (the auto-review transcript, the last request for `/context`, a pending fork, a cache key) does not outlive it | embedded |
| `io.Closer` | The session closes the engine with itself, stopping MCP servers and subagents | embedded |
| `Subagents` | The agent tools: `Attach` returns the tools to offer a run, `ToolNames` every name it answers, `Call` runs one, `Interrupt` stops a parent's children. The engine knows no tool name, schema, or result; [internal/agents](../agents/README.md) implements it | `internal/agents` |
| `Rewinder` | `Rewind` cuts a session's context before an earlier message, as Codex's backtrack (`Session.Rewind`), and returns `Rewound` and the texts that went to the agent with the message, which the session holds again | embedded |
| `Forker` | `Fork` copies a parent's history into a new child session for `spawn_agent`'s `fork_context`; `SetCacheKey` gives a session another prompt cache key (every subagent uses its root session's) | embedded |
| `Scoper` | `SetScope` narrows one session from its next run (`embedded/scope.go`): the tools it is offered, actions approved in advance (a role's `tools` and `approve`), and `NeverAsk`, which declines every action that would ask, before the auto-reviewer, for `/review`'s reviewer | embedded |

The engine's own events join the run's stream: `CompactionStarted`, `Compacted`, `AutoReviewed`, `AgentUpdated`, `AgentActivity` (a child's tool events, for the parent's view), `PatchApplied` (the diff of an applied `apply_patch` call, `patch.go`), `Rewound` (the session went back to before a message), `Reconnecting` and `ReconnectEnded` (a model request's retries, below), `TextDelta`, `ReasoningDelta`, and `StreamReset` (the answer as it arrives, below), and `WebSearch` (a hosted web search, below). The embedded engine's `Subagents()` returns its `Subagents`, so a session can follow one child's whole stream (`session.WatchAgent`).
<!-- /memoria:section -->

<!-- memoria:section id="varies" files="engine.go embedded/engine.go embedded/providers.go" -->
## What varies by provider and model

Every feature runs on the one engine. What still varies is the provider and the model, and uah says so where it matters:

| Feature | Varies by | Where |
| --- | --- | --- |
| `/fast` and `--fast` (priority processing) | Provider: openai and openai-codex accept `service_tier = "priority"` | `Provider.Priority` in `embedded/providers.go`. `Engine.Priority` reports it for the session's provider, which `/fast`, `/config`, and a subagent role's tier read; `embedded.Priority` lets `app.Resolve` refuse `--fast` or `fast = true` for another provider before an engine exists |
| Codex's `apply_patch` and its diffs | Model: the catalog entry's `apply_patch_tool_type`, and always on openai and openai-codex | `models.ApplyPatch`, read when a run builds its tools ([below](#the-tool-registry)); other models edit files with commands |
| Web search (`web_search`) | Provider: openai and openai-codex run the hosted tool | `Provider.WebSearch` in `embedded/providers.go`; other providers never get the tool ([below](#web-search)) |
| Automatic compaction | Model: its context window | `internal/compaction`'s window table, or `model_context_window` |

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
| Compaction and `/clear` | `embedded/compact.go` and `internal/compaction`; the session keeps the pending request (`internal/session/compact.go`) |
| `/context` | `embedded/context.go` (`ContextReporter`) |
| Going back to an earlier message | `embedded/rewind.go` (`Rewinder`, `cutStore`), the cut log in `internal/compaction/rewind.go`, and `internal/session/rewind.go` |
| `apply_patch` | `embedded/patchtool.go` and `internal/patch` |
| Session settings, saved and restored | `internal/session/saved.go` and `sidecar.go` |
| Model catalog | `internal/models`: the TUI's `/model` list, the subagents' model check, `apply_patch` per model, and the context window for compaction |
| Crash cleanup | uagent's `harness.Start` kills the tools a crashed run left behind, before the next run of the session (uagent v0.4.2) |
<!-- /memoria:section -->

<!-- memoria:section id="embedded" files="embedded/engine.go embedded/wiring.go embedded/agent.go embedded/adapter.go embedded/client.go embedded/providers.go embedded/clients.go embedded/reconnect.go embedded/stream.go embedded/websearch.go embedded/searchlog.go codexauth/codexauth.go embedded/store.go embedded/observer.go embedded/tools.go embedded/sandboxtool.go embedded/sandboxschema.go embedded/skills.go embedded/pretooluse.go embedded/autoreview.go embedded/compact.go embedded/context.go embedded/fork.go embedded/mode.go embedded/patchtool.go embedded/images.go embedded/rewind.go" -->
## The embedded engine

The embedded engine is a uagent `harness.Backend`. uagent still owns the run: the guards, the session lock, the run record, and the output stream. The backend (`wiring.go`) reproduces unreal-agent-runner v0.1.1's `Run` (`cmd/internal/agentrunner/run.go`) in the same order:

1. The provider client and the model (`client.go`, `providers.go`, a copy of the runner's provider table). `clients.go` builds each provider's Responses client as the runner's does, but over an HTTP client uah makes, so the engine can watch its retries (below). The ChatGPT credentials for openai-codex come from `codexauth`, which the model catalog (`internal/models`) and the usage reader share: the codex client's transport sets them on each request and refreshes them ([below](#the-chatgpt-login)).
2. The session store and the per-invocation log (`store.go`).
3. The tool registry (`tools.go`, below).
4. The operation manager with the remote job handlers.
5. The inbox, with the initial effort, the messages, and "stop when idle" (`agent.go`).
6. The context builder with the host prompt (uah's default prompt when the request has none), the skills, and the tools.
7. The coordinator, on its own goroutine. A panic in runner code becomes an error, so it cannot take down the TUI.

Every opened resource adds a closer; a failed start closes them in reverse, and after a successful start the coordinator's goroutine closes them when it returns.

The session store is the runner's own, under `<state>/sessions`, and uagent writes the run records, as when the runner binary runs. So a session the runner binary started resumes here too: the coordinator restores the runner's history from its session file (`TestEmbedded_ResumesAProcessSession`, and `TestRunResumesAProcessSession` through `uah run --session`).

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

### Streaming

The runner's client reads the SSE stream and drops its deltas, and nothing in the runner reports partial output (v0.1.1). With `Options.Stream`, the switcher gives each turn request a stream in its context (`stream.go`), and `watchTransport` tees each attempt's body into it: the runner reads the same bytes, and a line parser turns `response.output_text.delta` and `response.reasoning_summary_text.delta` into `engine.TextDelta` and `engine.ReasoningDelta` (`Final` from the message's `phase` in `response.output_item.added`). The tee only appends to a buffer; a pump goroutine emits the deltas, merged when they pile up, so a slow consumer never holds up the runner's read. Before the request returns, the stream sends what is left, so every delta precedes the runner's final `AssistantMessage` and `ReasoningSummary`, which stay authoritative.

A new attempt after text streamed, or a request that fails or is canceled after it, sends `engine.StreamReset`: that text is void. `switcher.direct()` carries no stream, so compaction summaries and the auto-reviewer never stream, and a subagent's session never asks. The [streaming design](../../docs/design/streaming.md) has the research and the reasons.

### Web search

With `Config.WebSearch` (`web_search = "live"`, the default), a run on a provider with `Provider.WebSearch` (openai, openai-codex) offers the hosted tool: `hostedTools` (`websearch.go`) adds `llm.Tool{Type: ToolHosted, Name: "web_search"}` to the context builder after the registry's tools, and the runner sends it as `{"type":"web_search"}`. Subagents' runs get it too. The runner drops a response's `web_search_call` items (v0.1.1), so every turn request of a run with events carries a stream even without `Options.Stream`, which then reports only searches: the tee decodes the `web_search_call` lines of `response.output_item.added` and `.done` into `engine.WebSearch` (started, then done with the action, query, URL, and pattern). The runner leaves the items out of the history, so the tee records each finished search with the ID of the output item it came before (`searchlog.go`, `sessions/<id>.websearch.jsonl`), and `watchTransport` inserts it before that item in every later turn request whose input has it, splicing bytes into the runner's body and changing nothing else. Compaction summaries and auto-review calls carry no stream and get none; a compaction, `/clear`, or a rewind removes the anchor and so the search; a fork copies the parent's log. The [web search design](../../docs/design/web-search.md) has the research, the probes, and the rationale.

### Forked sessions

`Fork` (`fork.go`) copies a parent's history into a new child session for `spawn_agent`'s `fork_context`: the items before the parent's turn that made the spawn call, through the runner store's append methods (its own `Store.Fork` drops the operation snapshots of inherited tool calls in v0.1.1), with unfinished operations recorded as canceled, and the parent's compactions until then. On the child's first run, `openStore` adds the run's messages and effort to the store before the coordinator restores it, because the coordinator asks the model at once for the copied inputs; the inbox then drops the messages as seen. [internal/agents](../agents/README.md#forking) describes the behavior.

### Going back to an earlier message

`Rewind` (`rewind.go`) reads the session's runner items, finds the message's inbox input by its ID, and cuts from it, or from the first of the inputs just before it, which reached the agent with it; their texts go back to the session to send again. It refuses a message that arrived while a tool call had no status, since the coordinator would run the call again. The cut is a `compaction.Rewind` in `sessions/<id>.rewind.jsonl`: the first and last runner item sequences, the message, and the context the last response before it reported. Every run then opens the store through `cutStore`, whose `Items` leaves the cut items out, so the coordinator restores the history without them; its `AppendTurn` chains a new turn to the file's latest turn, which the runner's store checks. `Fork` and the usage seed read the same filtered items. The compactor settles once per run which saved compaction applies: the newest that matches, skipping records made before a rewind that no longer match. The last request kept for `/context` is trimmed to before the message. See the [rewind design](../../docs/design/rewind.md).

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
| `uah.apply_patch` (version 1) | `patchJobs` | One approved patch: it reads the files, writes the changes, and completes with Codex's summary as the result and the diff (`engine.PatchHandle`) as the handle, which the session file keeps untruncated |

A job that had already started before the run stopped fails with "interrupted" when the session resumes, instead of running twice. Cancelling a job cancels its context. The calls of one model turn run at the same time, each on its own goroutine (`TestAgents_ParallelCalls`).
<!-- /memoria:section -->

<!-- memoria:section id="extending" files="engine.go embedded/tools.go embedded/providers.go embedded/wiring.go" -->
## Extending the engine

| To add | Touch |
| --- | --- |
| A live setting | A `Run` method in `engine.go`; `embedded/agent.go` delivers it; `internal/session/dispatch.go` (`onSettings`) calls it and falls back to the next run when it fails |
| A built-in tool | A registry layer in `embedded/tools.go`, wrapping the registry as the MCP and agent layers do. Use a remote job if the call can take long |
| A provider | `embedded/providers.go`, mirroring the runner's table, and its client in `embedded/clients.go`, built as the runner's client is, over `remoteHTTPClient`; set `Priority` if it accepts `service_tier = "priority"`, and add it to `TestClients_MatchTheRunner` |
| A session-level query | An optional interface in `engine.go`, implemented by the embedded engine and probed by `internal/session` |

The runner stays unchanged: uah reproduces its wiring instead of patching it, and the equivalence test below keeps the two in step.
<!-- /memoria:section -->

<!-- memoria:section id="tests" files="embedded/embedded_test.go embedded/approval_test.go embedded/compact_test.go embedded/compact_settings_test.go embedded/context_test.go embedded/mcp_test.go embedded/mcpjobs_internal_test.go embedded/sandbox_test.go embedded/mode_test.go embedded/images_test.go embedded/clients_test.go embedded/reconnect_test.go embedded/stream_test.go embedded/stream_internal_test.go embedded/rewind_test.go embedded/rewind_internal_test.go embedded/codexlogin_test.go codexauth/codexauth_test.go codexauth/login_test.go codexauth/file_internal_test.go embedded/websearch_test.go embedded/searchlog_internal_test.go" -->
## Tests

The tests run against `testing/fakellm`, a scripted Responses API, and need no tokens. The ones that compare with the real runner drive it through `harnesstest.RunnerEngine`, a test-only engine over uagent's harness that spawns a runner binary.

| Test | Pins |
| --- | --- |
| `TestEmbedded_MatchesTheRunner` | The real `unreal-agent-runner` (built from go.mod's version) and the embedded engine get the same script and must produce the same events and session items. `go test -short` skips it |
| `TestEmbedded_SteersALiveRun`, `TestEmbedded_ChangesSettingsLive`, `TestEmbedded_InterruptThenContinue` | Live input, live settings, and interrupts |
| `TestEmbedded_ResumesAProcessSession` | A session the real runner started resumes on the embedded engine with its history. `go test -short` skips it |
| `approval_test.go`, `sandbox_test.go` | Escalation, rules, "don't ask again", headless denial, PermissionRequest hooks, auto-review, and the sandbox |
| `mode_test.go` | Permission modes: a live switch to read only makes the next write fail in the sandbox and the next request describe it; Auto mode lets the reviewer allow or decline without asking, also once its breaker opens |
| `compact_test.go`, `compact_settings_test.go`, `clear_test.go`, `context_test.go` | Manual and automatic compaction, the configured summary model, prompt, focus, token limit, and kept-message cap, the stop when compacting cannot get under the limit, `/clear` in the same session, resume after both, the PreCompact hook, and `/context` |
| `mcp_test.go`, `mcpjobs_internal_test.go` | MCP tools, crashes, interrupts, approvals, and jobs that are not repeated |
| `clients_test.go` | Each provider's client sends the same request as the runner's own client, body and headers |
| `reconnect_test.go` | A connection dropped before the answer and one cut halfway (`fakellm.Reply.Drop`, `Cut`) are retried after 2 and 4 s with a `Reconnecting` event each, and the run then finishes; with two attempts that both drop, the run fails and says it gave up. They wait for the real backoff (about 6 s), so `-short` skips them |
| `stream_test.go`, `stream_internal_test.go` | With `fakellm.Reply.Deltas`, `Reasoning`, and `Hold`: the answer and reasoning arrive as deltas while the response is held, before the final message, which they match; no deltas without `Stream`, from a compaction summary, or from the auto-reviewer; a stream cut halfway is reset before the retry's text (skipped under `-short`), and an interrupt resets it too. The tee passes a stream read one byte at a time through unchanged and finds its deltas across reads, CRLF, and a line too long to parse |
| `websearch_test.go` | With `fakellm.Reply.Searches`: the hosted tool in every request when the engine offers it on a provider with search, and in none when it is off or the provider has none; a search and an opened page reported as they start and finish, before the answer, in a session that does not stream text; the next request with both searches before the answer they preceded, as Codex sends them; a resumed session keeping them; a rewind, a compaction, and `/clear` dropping them, and no search in a compaction's summary request. `searchlog_internal_test.go`: insertion before or after an anchor with the body otherwise byte for byte the same, and a request without an anchor unchanged |
| `rewind_test.go`, `rewind_internal_test.go` | Going back: the next request carries only the history before the message and the edited one, `/context` shrinks, the session file keeps the old branch, and resume and `session.Load` keep the cut; past a compaction that covered the message (the one before applies, no mismatch), after a `/clear` (the clear stays), an unknown message, a notification that went with the message and goes again, and where the cut starts or why it is refused |
| `codexlogin_test.go` | The ChatGPT login against a fake token endpoint: a 401 from `fakellm` renews the token and the request is sent again; an expired token is refreshed before the run; a refused refresh says to run `codex login` and is not retried |
| `codexauth/login_test.go`, `codexauth/file_internal_test.go`, `codexauth/codexauth_test.go` | Refreshing before expiry with Codex's request, the file written back in Codex's layout with unknown fields, mode 0600, and a rename; a 401 retried once; sixteen concurrent renewals making one request; a token Codex wrote before or during the refresh used instead; refusals, their message, and no second attempt; the environment's token never refreshed; `BeforeRun`; the lock file; and loading and checking the credentials |
| `images_test.go` | A message with pasted images: the model gets the text without tags and each image as a ViewImage result with its data URL, a missing image as an error text, and later requests carry the image again |
<!-- /memoria:section -->
