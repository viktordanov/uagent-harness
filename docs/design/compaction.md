# Compaction and the context meter: plan

Status: accepted, 2026-09-24 (ledger item 4). Embedded engine only.

1. [How Codex compacts](#how-codex-compacts)
2. [What the runner supports](#what-the-runner-supports)
3. [Design](#design)
4. [Open decisions](#open-decisions)

## How Codex compacts

Paths are in Codex `rust-v0.156.1`, `codex-rs/`.

- **The call** (`core/src/compact.rs`, `run_compact_task_inner_impl`). The current history plus one user message with the summarization prompt (`prompts/templates/compact/prompt.md`) goes to the session's model, without tools. The last assistant message is the summary.
- **The new history** (`build_compacted_history`). Every earlier user message stays as it was, in order, followed by one user message: `summary_prefix.md`, a newline, and the summary. Assistant messages, reasoning, tool calls, and tool outputs are gone. Earlier summaries are recognized by the prefix (`is_summary_message`) and dropped, because the new summary covers them. User messages are capped at 20,000 tokens in total, newest first (`COMPACT_USER_MESSAGE_MAX_TOKENS`).
- **When** (`core/src/session/turn.rs`, `core/src/session/context_window.rs`). Before a turn samples, and after a response that needs a follow-up (tool results), when the context in use reaches the limit: 90% of the model's context window (`ModelInfo::auto_compact_token_limit` in `protocol/src/openai_models.rs`), or `model_auto_compact_token_limit` from the configuration if lower. `/compact` runs the same task on demand.
- **Windows** (`models-manager/models.json`). 272,000 tokens for every current model except `gpt-daybreak-red-latest` (372,000); unknown models fall back to 272,000 (`models-manager/src/model_info.rs`). `model_context_window` in the configuration overrides it.
- **The meter** (`tui/src/token_usage.rs`). "N% context left" uses the last response's total tokens (input plus output) against the window, after subtracting a 12,000-token baseline from both, so a fresh session shows 100%.

## What the runner supports

unreal-agent v0.1.1 names a compaction turn (`harness/session/session.go`: `TurnCompaction`) but never creates one: `requestModelResponse` in `harness/coordinator/loop.go` always appends `TurnRegular`. When a compaction turn is replayed, the coordinator skips its model response (`loop.go:467`, `loop.go:610`) and changes nothing else: the context builder keeps every earlier item, so the turn would not shrink the context even if uah wrote one. `contextbuilder.ChangeCompacted` is a report label with no producer, and `Store.Fork` copies a session up to a turn without summarizing it. Nothing in the runner can be driven from outside to compact, so uah compacts on its own side and the runner stays unchanged.

## Design

The coordinator calls one `llm.Adapter` for every model request. The embedded engine already wraps it (`switcher`, for live model and tier). A second wrapper, `compactor` (`internal/engine/embedded/compact.go`), sits in front of it:

1. **Apply.** Each request the context builder produces is rewritten with the session's latest compaction: the system message, then every user message among the covered items verbatim and in order, then the summary message (Codex's prefix and the summary), then the items after the covered ones. A tool output whose call was covered becomes a user-role note, so the provider never sees an orphan output. The pure part lives in `internal/compaction` (`Apply`, `Summarize`'s request, the prompts, the window table).
2. **Compact.** Before sending, the compactor compacts when `/compact` asked for it or when the last response's total tokens reached `auto_compact_percent` of the window. It sends the rewritten history plus Codex's prompt through `internal/llmcall` with the session's model and effort and no tools, records the result, and rewrites the request with it. The call runs inside the coordinator's model call, so an interrupt cancels it like any model request. If it fails, the request goes out uncompacted and the failure is reported.
3. **Persist.** A compaction is a line in `sessions/<id>.compaction.jsonl`: how many builder items it covers, a SHA-256 of those items, the summary, the trigger, and the time. The builder's history is append-only between the system message and the end, so the covered items are the same on every later build and after a resume (the coordinator replays the session file into a fresh builder). On resume the compactor reads the last line; if the hash of the covered items does not match, it reports that and sends the full history.
4. **Events.** The engine emits `engine.CompactionStarted` and `engine.Compacted` (trigger, summary, error) into the run's event stream. The session passes them on, and the TUI shows "• Context compacted" (with the summary in the detailed view).
5. **Triggers.** `Session.Compact()` compacts before the live run's next model request, or before the first request of the next run when idle ("Compacting before the next message"). Automatic compaction needs no session involvement. Every compaction, manual or automatic, goes through `compactor.compact`, whose `BeforeCompact` callback (`embedded.Config.BeforeCompact`, unset today) is where item 9's `PreCompact` hook attaches.
6. **Meter.** The TUI keeps the last `ModelResponded` usage and shows "N% context left" in the footer (both views) with Codex's formula. The window is `model_context_window` if set, else the table's value for the current model. It works on both engines because it reads core events; a compaction clears it until the next response.
7. **Process engine.** `Capabilities.Compaction` is false; `/compact` says it needs the embedded engine. The runner process builds its own context and cannot be rewritten from outside.

`internal/llmcall` is a model-agnostic one-shot call: items in, text out, with the model, effort, and timeout per call, over any `llm.Adapter`. The embedded engine hands it an adapter that uses the session's current client (built by `Provider.NewClient`, so every provider works) without the live model override, so a caller can pick another model. Lane A's auto-reviewer uses the same package.

Configuration:

```toml
auto_compact_percent = 90       # 0 turns automatic compaction off
model_context_window = 272000   # tokens; default from the model table, 272000 for unknown models
```

## Open decisions

Defaults taken; the owner can change them.

| Decision | Default | Alternative |
| --- | --- | --- |
| User messages kept after compaction | All, verbatim, without Codex's 20,000-token cap | Codex's cap (oldest dropped or truncated) |
| Runner heartbeat messages (user-role) | Kept as user messages | Recognized and summarized |
| `/compact` while idle | Compacts before the next message's model request | An immediate compaction run, which needs a replay-only coordinator |
| Auto-compaction measure | Last response's input plus output tokens, as Codex | Last input tokens only |
| Summary model | The session's model and effort | A configurable cheaper model |
| Where the record lives | `sessions/<id>.compaction.jsonl` | The `<id>.uah.json` sidecar (metadata only today) |
