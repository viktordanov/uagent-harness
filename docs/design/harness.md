# Harness design: from wrapper to general-purpose harness

Status: accepted with a reduced scope, 2026-09-24 (proposed 2026-09-23). Evidence comes from five studies: the unreal-agent v0.1.1 source, web-tty's harness wrappers, Codex CLI 0.156.1, Claude Code 2.1.280, and a Go TUI framework benchmark ([bench/tui](../../bench/tui/README.md)).
Runner references use `RN/` for `github.com/unreallabsai/unreal-agent@v0.1.1`.
The code is split between uagent (events, the process engine, run records) and this repository (sessions, the embedded engine, instructions, hooks, the TUI); [implementation.md](implementation.md) places every file.

1. [Goal](#goal)
2. [Decisions for the owner](#decisions-for-the-owner)
3. [What the runner provides](#what-the-runner-provides)
4. [Two engines](#two-engines)
5. [Sessions](#sessions)
6. [Events](#events)
7. [Instructions](#instructions)
8. [Model, effort, and fast mode](#model-effort-and-fast-mode)
9. [Tools and file writes](#tools-and-file-writes)
10. [Hooks](#hooks)
11. [MCP](#mcp)
12. [Subagents, compaction, and safety](#subagents-compaction-and-safety)
13. [Scope](#scope)
14. [Roadmap](#roadmap)
15. [Upstream requests](#upstream-requests)

## Goal

uagent should match the harness features people rely on in Codex and Claude Code, without changing the runner's behavior.
The runner stays responsible for the agent loop, tools, retries, and prompt caching. uagent adds the layer around it: sessions, steering, instructions, hooks, and a stable event model for the CLI and the TUI. Features that change what the agent can do stay with the runner (see [Scope](#scope)).

The baseline both products share, ranked by user value:

1. Resume, continue, and fork by session ID, including headless.
2. One normalized event stream with a clear start and end, usage, and tool status.
3. Interrupt and steering: stop cleanly, and send a message while the agent works.
4. Instruction files (AGENTS.md, with CLAUDE.md compatibility).
5. Model and effort changes during a session, and a fast mode.
6. MCP servers, hooks, and a safe edit tool.
7. Compaction, subagents, sandboxing, and skills.

## Decisions for the owner

| # | Decision | Outcome |
| --- | --- | --- |
| D1 | How uagent drives the runner for interactive sessions | **Accepted:** add an **embedded engine** that imports the runner's public `harness/*` packages, next to today's **process engine**. See [Two engines](#two-engines). |
| D2 | What Enter does while the agent works | **Accepted:** Enter sends, and queues while the agent works; Ctrl+Enter steers immediately; Shift+Enter inserts a new line. A steer in this runner cancels the in-flight model request, so it is a deliberate key. |
| D6 | Which agent capabilities uagent builds itself | **Accepted:** uagent builds harness-level features and leaves agent behavior to the runner. See [Scope](#scope). |
| D3 | Where uagent's own configuration lives | `~/.config/uagent/config.toml` plus an optional `.uagent/config.toml` in the workspace for trusted projects. It holds defaults and hooks. |
| D4 | Whether to file the upstream requests | Yes. They let the process engine catch up, and the embedded engine does not depend on them. See [Upstream requests](#upstream-requests). |
| D5 | Hook format | Adopt the contract Codex and Claude Code share (JSON on stdin, exit 2 blocks, JSON output), so existing hook scripts work. |

## What the runner provides

| Capability | unreal-agent-runner v0.1.1 | Evidence |
| --- | --- | --- |
| System prompt | Preamble + skills + host prompt; `system_prompt` replaces only the host part | `RN/harness/contextbuilder/builder.go:33-86` |
| AGENTS.md / CLAUDE.md | Not loaded | no reference in the source |
| Skills | `<workspace>/.harness/skills/*/SKILL.md`, `SkillUse` tool | `RN/harness/tool/registry.go:153-224` |
| Session create and resume | `session_id`; history replayed into the model context, not to stdout | `RN/cmd/internal/agentrunner/run.go:647-668` |
| More messages in a session | `messages[]`, deduplicated by `message_id` | `run.go:365-395`, `RN/harness/inbox/local.go:73` |
| Live steering | In-process inbox only; the CLI reads stdin once and always queues "stop when idle" | `run.go:397-405, 483` |
| Effort change | `settings` control input, applied from the next model request | `builder.go:67-78` |
| Model change | Set once per process; not stored in the session | `run.go:408-411` |
| Fast mode (`service_tier`) | Not set; the adapter can merge extra fields through `Config.Extensions` | `RN/harness/llm/responsesapi/adapter.go:60-63` |
| Tools | `Bash` (background, own process group), `ViewImage`, `SkillUse` | `RN/harness/tool/static.go:39-88` |
| Edit or write tool | None; edits happen through Bash | |
| MCP, subagents, compaction, approvals, sandbox | None (`TurnCompaction` and `Store.Fork` exist but are unused) | `RN/harness/coordinator/loop.go:552` |
| Retries, token usage, prompt caching | Yes | `RN/harness/llm/responsesapi/retry.go`, `RN/harness/llm/model.go:126-135` |
| Token streaming | No (`include_partial_messages` is ignored, upstream issue #10) | `RN/harness/llm/responsesapi/stream.go:167-174` |
| Session file locking | None; two processes on one session corrupt it | `RN/harness/sessionstore/localfile/store.go:413-433` |

Everything below builds on this table: uagent adds what is missing without patching the runner.

## Two engines

The runner offers two integration surfaces. The harness supports both behind one `Engine` interface (`internal/engine` in this repository).

| | Process engine (today) | Embedded engine (new) |
| --- | --- | --- |
| How | Spawn `unreal-agent-runner`, JSON on stdin, decode JSONL | Import `RN/harness/{coordinator,inbox,sessionstore/localfile,llm/...,tool,operation}` and wire them like `run.go:144-447` (MIT, about 300 lines) |
| Steering | Queue until the process exits, or interrupt (SIGINT) and resume | `inbox.Submit` from any goroutine; running tools keep running |
| Effort change | Next invocation | Live, through a `settings` control input |
| Model change | Next invocation | Per request, through an `llm.Adapter` wrapper |
| Fast mode | Not possible | `service_tier: "priority"` through a custom Responses client with `Extensions` |
| Custom tools (edit, MCP, subagents) | Not possible | Own `tool.Registry` plus `operation.RemoteJobHandler` |
| Blocking hooks | Not possible (observe only) | Wrap tool translation and the inbox |
| Workspace `.env` | Loaded by the runner; uagent guards it | Never loaded; uagent controls the environment |
| Isolation | Runner crash cannot take uagent down | Runner code runs in uagent's process; tools stay separate processes |
| Coupling | Stable CLI contract | Tied to v0.1.x internals; pin the version |

Both engines read and write the same session file format (version 2), so a session started by one can be resumed by the other. That file format is the contract uagent relies on; the Go APIs behind it may change.

Recommendation: keep the process engine as the default for `uagent "<prompt>"` and scripts, where it matches the runner exactly. Use the embedded engine for the TUI and any long-lived session.
Every guard applies to both: state outside the workspace, the disk limit, the timeout, killing tool process groups, and credential checks.

## Sessions

A `Session` (`internal/session` in this repository) is the long-lived object the TUI talks to. uagent's `Harness.Run` stays as the one-shot path.

```go
s, err := h.OpenSession(ctx, harness.SessionOptions{ID: id, Settings: settings}) // ID "" creates one
s.Submit(core.UserInput{Text: "..."})   // start, steer, or queue, depending on state and engine
s.InterruptAndSend(input)               // stop the current step and deliver input now
s.Interrupt()                           // SIGINT or a hard control; keeps completed work
s.SetSettings(core.Settings{Model, Effort, ServiceTier}) // applied live or at the next run
events := s.Events()                    // ordered, lossless, buffered channel
s.Close()
```

Rules:

- **One runner per session.** Hold an advisory lock (`flock`) on `<state>/sessions/<id>.lock` for the life of a run. A second opener gets a clear error instead of a corrupted file.
- **Stable message IDs.** Every user input gets a UUID before it is queued. The runner echoes it in its `input` items, which is the delivery acknowledgement, and it deduplicates retries.
- **Queue semantics.** Inputs submitted while a run is busy are queued in order and delivered together as one `messages[]` request (process engine) or at the next inbox read (embedded engine).
- **Interrupt.** Send SIGINT first so the runner stops its coordinator cleanly (exit 130), then fall back to today's SIGTERM and SIGKILL teardown after the grace period. Tools that were running are recorded by the runner as interrupted failures on resume.
- **History.** `Sessions()` lists sessions grouped by session ID with their first prompt, run count, last activity, model, status, and tokens, reading only `request.json` and `summary.json`. `LoadSession(id)` concatenates each run's `events.jsonl` in start order; runner stdout holds only the items appended by that run, so concatenation rebuilds the transcript exactly. Runs without a summary (crashed or still running) are included from their request and events.
- **Fork.** `Fork(id, turn)` copies the session file up to a turn into a new ID. The runner's `Store.Fork` implements the copy; the coordinator's fork FIXME (`loop.go:552`) means a forked session must not end in an unanswered tool call.

## Events

The event model grows additively, so the stream keeps schema version 1. The decoder already sees these items and drops them; that is the first gap to close.

| New event | Source | Why |
| --- | --- | --- |
| `UserMessage{ID, Text}` | runner `input` item of kind `external` | The transcript needs the user's side; its ID acknowledges delivery |
| `ControlInput{ID, Mode, Effort}` | runner `input` item of kind `control` | Shows effort changes and stops |
| `SessionOpened{ID, Resumed}` | uagent | Emitted first, so a consumer has the session ID immediately |
| `InputQueued`, `InputDelivered` | uagent session | Drives the TUI's queue panel |
| `SettingsChanged{Applied: live or next_run}` | uagent session | Tells the user when a change takes effect |
| `InstructionsLoaded{Files}` | uagent | Shows which instruction files are in the prompt |
| `Idle` | embedded engine | "Ready for input" without the process exiting |

Existing events gain fields: `TurnID` and `Turn` on model and message events, raw `Arguments` on `ToolCalled`, and the operation type and output file paths on tool events (so the TUI can tail running commands).
Every new field and event is documented in `stream/README.md` and covered by golden files.

## Instructions

The runner ignores instruction files, so uagent assembles them into `system_prompt`:

1. Walk from the Git root (or the workspace when there is no repository) down to the workspace. In each directory take `AGENTS.override.md`, else `AGENTS.md`; if neither exists, take `CLAUDE.md`.
2. Prepend `~/.config/uagent/AGENTS.md` (or `~/.codex/AGENTS.md` when uagent has none).
3. Join them root first, so closer files come later and win, and stop at 32 KiB, like Codex.
4. Build `system_prompt` as the runner's own default host prompt followed by the instruction files. The runner replaces its host prompt entirely when `system_prompt` is set, so uagent must keep that default text itself.
5. Emit `InstructionsLoaded`, and support `--no-instructions` and `--instructions-file`.

User-level skills follow the same idea: the runner only reads `<workspace>/.harness/skills`, so uagent can link skills from `~/.config/uagent/skills` into a per-run copy of that directory. This stays opt-in, because it writes into the workspace.

## Model, effort, and fast mode

- **One validation gate.** Effort must be one of `low`, `medium`, `high`, `xhigh`, `max`. Model IDs are checked for syntax only (no leading `-`, no control characters); the provider stays the authority on which models exist. A provider without a default model needs `--model` (done).
- **Effort.** Process engine: next run. Embedded engine: a `settings` control input, applied from the next model request.
- **Model.** Process engine: next run with a different `model`. Embedded engine: an adapter wrapper swaps `request.Model` per call.
  Risk: the session replays earlier encrypted reasoning items verbatim, and another model may reject them. Test this with real captures first; if it fails, strip foreign reasoning items in a context-builder wrapper (embedded) or start a fork on model change (process).
- **Fast.** Codex maps `/fast` to `service_tier: "priority"`. The embedded engine can send it through `responsesapi.Config.Extensions` with its own OpenAI and Codex clients, one adapter per tier. Whether the Codex subscription backend accepts it for every account is unverified. The process engine needs upstream request (b).

## Tools and file writes

Today the model edits files through Bash (heredocs, `sed`), which is neither atomic nor reviewable.
The embedded engine can add first-class tools through its own registry:

- **`Edit`**: exact-string replacement (Claude Code's contract: the old text must be unique, or `replace_all` is set). **`Write`**: create or overwrite a file. Both report a `FileChanged{Path, Kind, Diff}` event.
- **Atomic, efficient writes**: write to a temporary file in the same directory, `fsync` it, `rename` it over the target, and `fsync` the directory. Keep the file mode. Skip the write when the content hash is unchanged. For many edits in one step, stage every file first and rename them together at the end, so a failure leaves nothing half-applied.
- **Checkpoints**: before the first write to a path in a turn, store its previous content in a content-addressed store under the state directory. `/rewind` restores files and forks the conversation at that turn. Bash changes are not tracked, as in Claude Code; a `git stash`-based snapshot can cover them later.

## Hooks

Hooks use the contract Codex and Claude Code share: the event JSON arrives on stdin; exit 0 continues; exit 2 blocks with stderr as the reason; JSON output can carry `permissionDecision` (`allow`, `deny`, `ask`), `updatedInput`, and `additionalContext`.

| Event | Process engine | Embedded engine |
| --- | --- | --- |
| `SessionStart`, `SessionEnd` | Yes | Yes |
| `UserPromptSubmit` (can block or add context) | Yes, before the run starts | Yes, before `inbox.Submit` |
| `PreToolUse` (allow, deny, rewrite input) | No | Yes, around tool translation |
| `PostToolUse` | Observe only, after the fact | Yes |
| `Stop` (block to keep the agent working) | Resubmit a message after the run | Yes, on `Idle` |
| `PreCompact` | When compaction exists | When compaction exists |

Hooks are configured in uagent's config files. A project hook runs only after the user trusts it: uagent records a hash of the hook command, as Codex does, and asks again when the command changes. Hook runs time out (default 60 s, 1 s for `SessionEnd`).
Every hook input carries `session_id`, `run_id`, `cwd`, `model`, `effort`, and `transcript_path`, so scripts written for Codex or Claude Code adapt easily.

## MCP

The runner rejects MCP configuration (`RN/cmd/unreal-agent-runner/tools_test.go:43-58`), so MCP needs the embedded engine:

- Config: `[mcp_servers.<name>]` in uagent's config, with the Codex shape (`command`, `args`, `env`, or `url`, `bearer_token_env_var`, `enabled_tools`, `startup_timeout_sec`, `tool_timeout_sec`). Import a project `.mcp.json` when present, so Claude Code projects work.
- Transports: stdio and streamable HTTP, through the official Go SDK (`github.com/modelcontextprotocol/go-sdk`) or `mark3labs/mcp-go`.
- Tools appear as `mcp__<server>__<tool>`. Each call runs as a runner operation through a `RemoteJobHandler`, so it is asynchronous like every other tool, and it passes through `PreToolUse` hooks.
- Approval: per-server and per-tool allow lists; tools that declare destructive hints need approval unless allowed.

## Subagents, compaction, and safety

- **Subagents (later).** An `Agent` tool in the embedded engine starts a child coordinator on a new session (optionally forked from the parent), runs it to idle, and returns its answer. Child events carry `ParentCallID`, and the parent's turn is not complete while children run (a lesson from web-tty, where Claude Code's early Stop needed a workaround).
- **Compaction (later).** The runner never compacts and fails on `context_length_exceeded`. uagent can offer `/compact`: ask the model for a summary, start a new session seeded with it, and link the two. Automatic compaction follows once token counts approach a model's limit.
- **Safety.** The runner has no sandbox or approvals, and its maintainers position it for trusted environments. uagent keeps its guards; stronger isolation means running the runner in a container or a macOS sandbox profile, which is a separate project.

## Scope

uagent stays true to the runner: it owns the layer around the agent, and the runner owns what the agent can do.

| Feature | Owner | Status |
| --- | --- | --- |
| Sessions, locking, queue, steering, history | uagent | Planned |
| AGENTS.md assembly, configuration | uagent | Planned: it only builds `system_prompt` |
| Embedded engine (live effort, model, fast mode) | uagent | Planned |
| Hooks | uagent | Planned |
| Guards, statistics, TUI | uagent | Done or planned |
| MCP | Runner | Deferred: upstream has an unpublished MCP runner ("Isolate MCP and add a slim unreal-agent runner") |
| Edit and Write tools | Runner | Deferred: Bash edits work today; revisit if bad edits appear in practice |
| Compaction | Runner | Deferred: upstream is exploring it (issue #7); a simple `/compact` can come later if needed |
| Subagents | Runner | Deferred |

The sections on tools, MCP, subagents, and compaction above describe how uagent could add them if upstream does not. They are not planned work.

## Roadmap

| Phase | Scope | Size |
| --- | --- | --- |
| 1. Library foundations | Decode `input` items (`UserMessage`, `ControlInput`), richer tool events, `SessionOpened`, `Sessions()` and `LoadSession()`, the session lock, a `Session` with a queue on the process engine, SIGINT-first interrupt | 2–3 days |
| 2. Instructions and config | AGENTS.md assembly, `~/.config/uagent/config.toml`, `InstructionsLoaded` | 1 day |
| 3. TUI v1 | See [tui.md](tui.md): chat, live view, queue, `/model`, `/effort` (next run), `/new`, `/resume`, history | 4–6 days |
| 4. Embedded engine | Wiring, `Engine` interface, live steering, live effort, per-request model, fast mode, cross-engine resume tests | 3–4 days |
| 5. Hooks | The hook runner and trust store; blocking hooks on the embedded engine | 2 days |
| Deferred | MCP, `Edit` and `Write` with checkpoints, compaction, subagents, sandboxing, user-level skills (see [Scope](#scope)) | |

Each phase ships with fixtures captured from real runs (resumed sessions, multi-message requests, steering, reasoning summaries), following `testing/README.md`.

## Upstream requests

These would let the process engine match the embedded one, and cover the deferred features. File them with unreal-agent:

- (a) An input-stream mode: after the request, keep reading JSONL inbox inputs from stdin, and submit "stop when idle" only at EOF (the change is local to `run.go:397-405`).
- (b) A `service_tier` request field wired into the OpenAI and Codex clients.
- (c) Treat SIGTERM like SIGINT (`RN/cmd/unreal-agent-runner/main.go:13` traps only SIGINT).
- (d) Lock session files, or document one process per session.
- (e) Honor `include_partial_messages` for token streaming (issue #10).
- (f) Publish the MCP-capable runner, or its MCP tool support.
- (g) Share plans for compaction (issue #7), an edit tool, and subagents, so uagent does not duplicate them.
