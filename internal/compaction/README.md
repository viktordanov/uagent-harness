<!-- memoria:section id="overview" files="compaction.go" -->
# Compaction

<!-- memoria:export id="summary" -->
uah compacts a long conversation as Codex does: the earlier user messages stay verbatim and in order, up to the newest 20,000 tokens of them, and the rest is replaced by a model-written handoff summary. The session file keeps the full history; only what goes to the model changes, and a compaction is saved next to the session so a resumed session keeps it.
<!-- /memoria:export -->

This package holds everything about compaction that does not depend on the engine: the request rewrite, the summary call over any runner `llm.Adapter`, the token estimates, the context window table, and the compaction log. The embedded engine (`internal/engine/embedded/compact.go`) decides when to compact, runs the summary call, and emits the events. The facts about Codex were checked against Codex `rust-v0.156.1`, and the facts about the runner against unreal-agent v0.1.1.

1. [The request rewrite](#the-request-rewrite)
2. [The summary call](#the-summary-call)
3. [Persistence and resume](#persistence-and-resume)
4. [Triggers](#triggers)
5. [The context meter](#the-context-meter)
6. [Failures](#failures)
7. [Extension points](#extension-points)
<!-- /memoria:section -->

<!-- memoria:section id="rewrite" files="compaction.go keep.go" -->
## The request rewrite

The runner's context builder produces every model request: the system message, then the history in order. The builder is append-only after each turn, so the first items of a request stay the same on every later request and after a resume. A compaction `Record` covers the first `Covered` items after the system message. It covers everything except the user messages at the end of the request, because they are new input and follow the summary, as in Codex.

`Apply` rewrites a request with a record:

1. The system message, unchanged. It can change between requests (instructions, skills) without breaking the record.
2. The user messages among the covered items that `Kept` keeps: the newest ones up to 20,000 tokens (`UserMessageMaxTokens`, Codex's `COMPACT_USER_MESSAGE_MAX_TOKENS`). The message that crosses the cap is shortened in the middle with Codex's marker, "…N tokens truncated…". Older messages are left to the summary. The runner's heartbeat messages ("Heartbeat: waited …") are not the user's, so they are left to the summary too.
3. The summary message: a user message with Codex's `summary_prefix.md`, a newline, and the summary.
4. The items after the covered ones, unchanged, except a tool result whose call was covered. That result becomes a user message ("Output of the earlier tool call …"), so the provider never sees an output without its call. An image in it is named, not sent.

An example. The request at the time of `/compact`:

```text
system
user       "fix the build"                        covered
tool call  c1 Bash {"command":"go build ./..."}   covered
tool result c1 "main.go:3: undefined: x"          covered
assistant  "I fixed main.go."                     covered
user       "also run the tests"                   new input
```

The request after it, and every later one, starts like this:

```text
system
user       "fix the build"
user       "<summary prefix>\n<summary>"
user       "also run the tests"
...        (the items that follow)
```

A second compaction covers more items and replaces the first summary. Its summary call sees the first summary, so nothing is lost; the user messages still come from the covered items, so they stay verbatim.
<!-- /memoria:section -->

<!-- memoria:section id="summary-call" files="summary.go prompts/prompt.md prompts/summary_prefix.md" -->
## The summary call

`SummaryRequest` builds the call from the history as the model sees it (with the latest compaction applied): the system text becomes the call's instructions, then the history, then Codex's `prompt.md` as a user message. `Summarize` sends it through `internal/llmcall` with no tools, the session's model and effort, the session as the prompt cache key, and llmcall's timeout (5 minutes).

The history can be larger than the window, for example when one tool output filled it. `Summarize` drops the oldest items until the estimate fits the window next to the prompt and the instructions, as Codex drops the oldest history when a summary call overflows. When the provider still reports an overflow (`llmcall.ErrContextWindow`), it retries with three quarters of what it sent, at most four times. A tool result whose call was dropped becomes a user message, as in the rewrite.

The prompts are Codex's, under the Apache License 2.0 (`prompts/LICENSE-codex`).
<!-- /memoria:section -->

<!-- memoria:section id="persistence" files="log.go compaction.go" -->
## Persistence and resume

A compaction is one JSON line in `sessions/<id>.compaction.jsonl`, next to the runner's session file: the number of covered items, a SHA-256 hash of them, the summary, the trigger, the model, and the time. The last readable line applies. `Log.Append` syncs the file, and it ends a line that a crash cut short before it writes, so the new line stays readable.

A resumed run replays the session file into a fresh builder, which produces the same covered items, so the record applies again. Before each request, `Apply` checks the hash. When the history does not match (the session file was changed outside uah), the engine sends the full history and reports the mismatch once. A line that does not decode is skipped and logged, so one bad line does not stop a session from resuming.

The log is also the source for a reloaded transcript: `session.Load` adds each record as an `engine.Compacted` event to the run it happened in. The runner's `events.jsonl` stays as the runner wrote it.
<!-- /memoria:section -->

<!-- memoria:section id="triggers" files="estimate.go window.go" -->
## Triggers

- **Manual.** `/compact` compacts before the live run's next model request, or before the first request of the next run when the session is idle.
- **Clear.** `/clear` records a compaction with the `clear` trigger and no summary (`NewClear`): every item so far is dropped from what the model sees, with no model call, and the session and its file stay the same. Its `Floor` is its `Covered`; `Apply` leaves the items below a record's floor out entirely, and a later compaction carries the floor forward, so a summary after a clear keeps only the user messages after it. Codex and Claude Code start a new session for `/clear`; uah stays in the session, and `/new` starts a new one.
- **Automatic.** Before each model request, when the context in use reaches `auto_compact_percent` of the window (`AutoLimit`; 90 by default, as Codex) and there is something new to cover. The second condition stops a history that stays large after a compaction from compacting on every request.

The context in use is Codex's measure (`InUse`, after `get_total_token_usage`): the last response's total tokens plus an estimate of the items added after the last item the model produced, such as tool outputs and new messages. When the last response reported no usage (a provider without usage, or the first request after a compaction), the whole request is estimated. The estimate is Codex's: the model-visible bytes divided by four, 7,373 bytes for an image, and three quarters of the encoded length less 650 for encrypted reasoning.

The window comes from `ContextWindow`, the one function every caller uses: `model_context_window` when set, else the model catalog's value (the provider's list, cached, or Codex's bundled catalog; see `internal/models`), else this package's table, else 272,000 tokens.

The engine runs a compaction as a job under the run, not under the request. The runner cancels a model request when a message arrives; the next request then waits for the same job instead of starting a second summary. An interrupt cancels the job, and the request that waited does not go out.
<!-- /memoria:section -->

<!-- memoria:section id="meter" files="window.go" -->
## The context meter

`PercentLeft` is Codex's "N% context left": it treats 12,000 tokens as always in use (the system prompt and tools), so a fresh session shows 100%.

```text
effective = window − 12,000
left      = round(100 × max(effective − max(used − 12,000, 0), 0) / effective)
```

The TUI uses the last response's input plus output tokens as `used`. A compaction clears the meter until the next response.
<!-- /memoria:section -->

<!-- memoria:section id="failures" files="summary.go log.go" -->
## Failures

A failed compaction never loses the request: it goes out uncompacted, and the engine reports the failure (`engine.Compacted` with `Err`).

| Failure | What happens |
| --- | --- |
| The provider rejects the summary call, or it times out | Reported; the request goes out uncompacted. |
| The model answers without text | Reported as a failure (`llmcall.ErrNoText`); Codex would store "(no summary available)" and drop the history, which loses more. |
| The summary input overflows the window | Trimmed and retried, as in [The summary call](#the-summary-call). |
| Automatic compaction fails three times in a row | Automatic compaction stops for the rest of the run; `/compact` still works. |
| The user interrupts during the summary | The call is canceled, reported as interrupted, and no request goes out. |
| A `PreCompact` hook blocks it | Reported as stopped; the request goes out uncompacted. |
| The history does not match the saved record | The full history goes out; the mismatch is reported once. |
| The log has an unreadable line | The line is skipped and logged. |
<!-- /memoria:section -->

<!-- memoria:section id="extension" files="summary.go compaction.go" -->
## Extension points

- **Another summary strategy.** A `Summarizer` takes the history as the model sees it and returns the summary text. The engine's compactor uses `Summarize` unless its `summarize` field is set, so a cheaper model or a different prompt plugs in there without touching the rewrite.
- **A remote compaction endpoint.** An endpoint that returns summary text is a `Summarizer`. One that returns opaque items (such as encrypted compaction items) needs an item type that the runner's `llm.Item` (v0.1.1) does not have.
- **Another rewrite.** `Record` and `Apply` are pure functions of the builder's items. A different rewrite keeps the same contract: a record covers a prefix of the history, and its hash guards it.
- **Hooks.** The engine's `BeforeCompact` callback runs as each compaction starts; the `PreCompact` hook attaches there.
<!-- /memoria:section -->
