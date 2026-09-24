# Compaction and the context meter: plan

Status: accepted, 2026-09-24 (ledger item 4). Embedded engine only.

1. [How Codex compacts](#how-codex-compacts)
2. [What the runner supports](#what-the-runner-supports)
3. [Design](#design)
4. [Validation](#validation)
5. [Open decisions](#open-decisions)

## How Codex compacts

Paths are in Codex `rust-v0.156.1`, `codex-rs/`.

- **The call** (`core/src/compact.rs`, `run_compact_task_inner_impl`). The current history plus one user message with the summarization prompt (`prompts/templates/compact/prompt.md`) goes to the session's model, without tools. The last assistant message is the summary.
- **The new history** (`build_compacted_history`). Every earlier user message stays as it was, in order, followed by one user message: `summary_prefix.md`, a newline, and the summary. Assistant messages, reasoning, tool calls, and tool outputs are gone. Earlier summaries are recognized by the prefix (`is_summary_message`) and dropped, because the new summary covers them. User messages are capped at 20,000 tokens in total, newest first (`COMPACT_USER_MESSAGE_MAX_TOKENS`).
- **When** (`core/src/session/turn.rs`, `core/src/session/context_window.rs`). Before a turn samples, and after a response that needs a follow-up (tool results), when the context in use reaches the limit: 90% of the model's context window (`ModelInfo::auto_compact_token_limit` in `protocol/src/openai_models.rs`), or `model_auto_compact_token_limit` from the configuration if lower. `/compact` runs the same task on demand.
- **Windows** (`models-manager/models.json`). 272,000 tokens for every current model except `gpt-daybreak-red-latest` (372,000); unknown models fall back to 272,000 (`models-manager/src/model_info.rs`). `model_context_window` in the configuration overrides it.
- **The measure** (`core/src/context_manager/history.rs`, `get_total_token_usage`). The context in use is the last response's total tokens plus an estimate (bytes/4) of the items added after the last item the model produced. The trigger compares this, not the last response alone, with the limit.
- **Overflow** (`compact.rs`). When the summary call exceeds the window, Codex removes the oldest history item and retries. Other errors are retried with backoff up to the provider's stream retry limit.
- **The meter** (`tui/src/token_usage.rs`). "N% context left" uses the last response's total tokens (input plus output) against the window, after subtracting a 12,000-token baseline from both, so a fresh session shows 100%.

## What the runner supports

unreal-agent v0.1.1 names a compaction turn (`harness/session/session.go`: `TurnCompaction`) but never creates one: `requestModelResponse` in `harness/coordinator/loop.go` always appends `TurnRegular`. When a compaction turn is replayed, the coordinator skips its model response (`loop.go:467`, `loop.go:610`) and changes nothing else: the context builder keeps every earlier item, so the turn would not shrink the context even if uah wrote one. `contextbuilder.ChangeCompacted` is a report label with no producer, and `Store.Fork` copies a session up to a turn without summarizing it. Nothing in the runner can be driven from outside to compact, so uah compacts on its own side and the runner stays unchanged.

## Design

The coordinator calls one `llm.Adapter` for every model request. The embedded engine already wraps it (`switcher`, for live model and tier). A second wrapper, `compactor` (`internal/engine/embedded/compact.go`), sits in front of it:

1. **Apply.** Each request the context builder produces is rewritten with the session's latest compaction: the system message, then the user messages among the covered items verbatim and in order (the newest 20,000 tokens of them, without the runner's heartbeats), then the summary message (Codex's prefix and the summary), then the items after the covered ones. A compaction covers everything except the user messages at the end of the request, which are new input and follow the summary, as in Codex, where compaction runs before the new message is recorded. A tool output whose call was covered becomes a user-role note, so the provider never sees an orphan output. Everything that does not depend on the engine lives in `internal/compaction` (the rewrite, the summary call, the estimates, the window table and meter formula, the log); its [README](../../internal/compaction/README.md) describes it.
2. **Compact.** Before sending, the compactor compacts when `/compact` asked for it or when the context in use (Codex's measure) reached `auto_compact_percent` of the window. It sends the covered history (as the model saw it, so an earlier summary is included), trimmed to the window, plus Codex's prompt through `internal/llmcall` with the session's model and effort and no tools, records the result, and rewrites the request with it. The call runs as a job under the run's context: a request the coordinator cancels for a new message leaves it running, and the next request waits for it. An interrupt cancels it. If it fails, the request goes out uncompacted and the failure is reported.
3. **Persist.** A compaction is a line in `sessions/<id>.compaction.jsonl`: how many builder items it covers, a SHA-256 of those items, the summary, the trigger, and the time. The builder's history is append-only between the system message and the end, so the covered items are the same on every later build and after a resume (the coordinator replays the session file into a fresh builder). On resume the compactor reads the last readable line; if the hash of the covered items does not match, it reports that and sends the full history.
4. **Events.** The engine emits `engine.CompactionStarted` and `engine.Compacted` (trigger, summary, error) into the run's event stream. The session passes them on; the TUI shows "Context compacted" (with the summary in the detailed view), and `uah run` prints a progress line. `uah run --stream` writes them as `compaction_started` and `compacted`. A reloaded transcript (the TUI, `uah sessions show`) takes them from the compaction log, because the run records hold only the runner's own events.
5. **Triggers.** `Session.Compact()` compacts before the live run's next model request, or before the first request of the next run when idle (the TUI says "The context will be compacted before the next message"). A request the live run ends without serving moves to the next run. A new run reads the last response's usage from the session file, so automatic compaction also applies after a resume. Automatic compaction needs no session involvement. Every compaction, manual or automatic, goes through `compactor.compact`, whose `BeforeCompact` callback (`embedded.Config.BeforeCompact`, unset today) is where item 9's `PreCompact` hook attaches.
6. **Meter.** The TUI keeps the last `ModelResponded` usage and shows "N% context left" in the footer (both views) with Codex's formula. The window is `model_context_window` if set (carried in `session.Settings.ContextWindow`), else the table's value for the current model. It works on both engines because it reads core events; a compaction clears it until the next response.
7. **Process engine.** `Capabilities.Compaction` is false; `/compact` says it needs the embedded engine. The runner process builds its own context and cannot be rewritten from outside.

`internal/llmcall` is a model-agnostic one-shot call: items in, text out, with the model, effort, and timeout per call, over any `llm.Adapter`. The embedded engine hands it an adapter that uses the session's current client (built by `Provider.NewClient`, so every provider works) without the live model override, so a caller can pick another model. Lane A's auto-reviewer uses the same package.

Configuration:

```toml
auto_compact_percent = 90       # 0 turns automatic compaction off
model_context_window = 272000   # tokens; default from the model table, 272000 for unknown models
```

## Validation

Checked on 2026-09-24 against Codex `rust-v0.156.1` and the runner (unreal-agent v0.1.1). Each fix has a test through the real engine and `fakellm`; the compaction tests pass with `-race -count=20`.

| # | Finding | Severity | Status |
| --- | --- | --- | --- |
| 1 | The automatic trigger used only the last response's tokens. A large tool output after it could push the next request past the window without a compaction, and the request failed. Codex adds an estimate of the items after the last model item. | High | Fixed: `compaction.InUse`. |
| 2 | A provider that reports no usage (or a request right after a compaction) never triggered automatic compaction. | High | Fixed: the whole request is estimated when usage is missing. |
| 3 | The summary call sent the whole history even when it exceeded the window (one huge tool output, or a mismatched record that sends the full history), so it failed exactly when it was needed. | High | Fixed: trimmed to the window from the oldest item, and retried with less on a context-length error, as Codex. |
| 4 | The coordinator cancels the model request when a message arrives. A message sent during the summary canceled it: `/compact` was lost (reported as "failed: context canceled") and an automatic compaction started over, paying twice. | High | Fixed: the compaction is a job under the run's context that the next request joins. |
| 5 | An interrupt during the summary was reported as a failure, and the request could still go out. | Medium | Fixed: reported as interrupted (`engine.Compacted.Interrupted`); the waiting request does not go out; the run waits for the job before it ends. |
| 6 | One unreadable line in the compaction log (a write cut short by a crash) stopped every later run of the session from starting. | High | Fixed: unreadable lines are skipped and logged; a cut line is ended before the next append; appends are synced. |
| 7 | Codex's 20,000-token cap on kept user messages was not applied, so very long messages could fill the window again right after a compaction. | Medium | Fixed: Codex's selection and middle truncation, with Codex's marker. |
| 8 | The runner's heartbeat messages were kept as user messages, with stale lists of running calls. Codex keeps only real user messages. | Low | Fixed: heartbeats are left to the summary. |
| 9 | An automatic compaction whose summary call keeps failing ran before every model request. | Medium | Fixed: it stops for the run after three failures in a row. |
| 10 | Compactions were missing from reloaded transcripts and `uah run --stream`. | Medium | Fixed: see Events above. |
| 11 | A late output of a covered call became a note without its images. | Low | Fixed: the note names each image. |
| 12 | Multiple compactions: the second summary call sees the first summary, and the rewrite replaces it. | — | Verified; test. |
| 13 | A tool call still running at the compaction: its placeholder is covered, and its final output arrives later as a note, never as an orphan output. | — | Verified; test. |
| 14 | The covered items are stable: the builder commits a request's items when the turn starts, before the model call, and later tool results only append. A resume replays the same items. | — | Verified in the runner (`coordinator/loop.go`, `contextbuilder/builder.go`); resume and mismatch tests. |
| 15 | A summary with no text fails the compaction; Codex stores "(no summary available)" and drops the history. | Low | Kept: failing keeps the full history, which loses less. |
| 16 | Images in user messages: the runner's `llm.Message` carries text only (v0.1.1), so user messages have no images to keep. | — | Not applicable. |
| 17 | Each request hashes the covered items (JSON and SHA-256 of the history). | Low | Kept: a few milliseconds per MB of history, well under a model call. |

## Open decisions

Defaults taken; the owner can change them.

| Decision | Default | Alternative |
| --- | --- | --- |
| User messages kept after compaction | Codex's cap: the newest 20,000 tokens, the one that crosses it shortened in the middle | All, verbatim |
| Runner heartbeat messages (user-role) | Summarized, as Codex keeps only real user messages | Kept as user messages |
| A summary with no text | The compaction fails and the history stays | Codex's "(no summary available)" |
| Automatic compaction after failures | Stops for the run after three in a row | Keep trying before every request |
| `/compact` while idle | Compacts before the next message's model request | An immediate compaction run, which needs a replay-only coordinator |
| Auto-compaction measure | Last response's input plus output tokens, as Codex | Last input tokens only |
| Summary model | The session's model and effort | A configurable cheaper model |
| Where the record lives | `sessions/<id>.compaction.jsonl` | The `<id>.uah.json` sidecar (metadata only today) |
