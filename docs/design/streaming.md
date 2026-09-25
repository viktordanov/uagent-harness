# Streaming the answer

Ledger item 45: the agent's answer, and its reasoning summaries when the TUI shows them, appear as the model writes them, not all at once when the response ends. The runner stays unchanged.

Status: built, 2026-09-25, on the embedded engine.

1. [What Codex does](#what-codex-does)
2. [What the runner allows](#what-the-runner-allows)
3. [The design](#the-design)
4. [Which requests stream](#which-requests-stream)
5. [Retries and failures](#retries-and-failures)
6. [Out of scope](#out-of-scope)

## What Codex does

Checked against Codex rust-v0.156.1. Paths are under `codex-rs/`.

- **The stream.** `codex-api/src/sse/responses.rs:365-400` turns `response.output_text.delta` into `ResponseEvent::OutputTextDelta` and `response.reasoning_summary_text.delta` into `ReasoningSummaryDelta` with its `summary_index`; `response.output_item.added` (`:516`) opens an item.
- **The events.** `core/src/session/turn.rs:2878-2950` sends each delta to the client as `AgentMessageContentDelta` or `ReasoningContentDelta`, with the active item's ID. A delta without an active item is an error.
- **Retries.** The turn's sampling loop retries a failed stream as a whole request (`turn.rs:1611-1690`, `handle_response_stream_error`); the TUI shows the retry as a status line (`tui/src/chatwidget/streaming.rs:393-403`).
- **The answer in the TUI.** `tui/src/streaming/` holds the stream: `markdown_stream.rs` commits source only at newlines, `controller.rs` splits the rendered markdown into a stable region, committed to scrollback line by line, and a mutable tail, and `chunking.rs` with `commit_tick.rs` drains the committed lines smoothly, or all at once when they pile up. When the item completes, the completed message replaces the streamed source if they differ (`tui/src/chatwidget/streaming.rs:86-130`).
- **Reasoning in the TUI.** Reasoning deltas do not stream into the transcript: the latest bold heading of the summary becomes the status line's header (`streaming.rs:302-344`), and the summary joins the transcript as one block when the item ends (`:346-383`).

## What the runner allows

Checked against unreal-agent-runner v0.1.1.

- The Responses client reads the whole SSE stream in `harness/llm/responsesapi/stream.go` (`exchangeAttempt`, `responseState.observe`) and keeps only `response.output_item.done`, `response.completed`, `response.failed`, `response.incomplete`, and `error`. Deltas are parsed and dropped.
- `llm.Adapter` is one call, `Respond(ctx, req, opts) (Response, error)`, and `responsesapi.Config.Trace` sees only the request and the terminal body. No callback, observer, or event reports partial output, and the coordinator records a model response only once it is complete.
- The client retries inside `Respond` (`exchange`): a lost connection, a retryable status, a stream that ends early, or an in-band failure sends the whole request again. Each attempt is a new HTTP request through the `*http.Client` it was built with.
- The embedded engine builds each provider's client over its own `*http.Client` (`internal/engine/embedded/clients.go`, from item 41), whose transport sees every attempt (`reconnect.go`). The transport can wrap a response body and read the same bytes the runner reads, without changing them.

So the runner offers no hook, and none is needed: the transport sees the stream.

## The design

**Tee the body.** For a request that should stream, `watchTransport` wraps a 2xx response's body in a tee (`internal/engine/embedded/stream.go`). Each `Read` returns the base body's bytes unchanged; the tee also feeds them to a small SSE line parser. The parser keeps only `data:` lines and decodes only three event types:

| Event | Becomes |
| --- | --- |
| `response.output_text.delta` | `engine.TextDelta{ItemID, Text, Final}` |
| `response.reasoning_summary_text.delta` | `engine.ReasoningDelta{ItemID, Part, Text}` |
| `response.output_item.added` of a message | nothing; its `phase` sets `Final` on the item's deltas |

A line longer than 1 MiB is skipped: deltas are small, and the large lines are the final item and the completed response, which the runner reads.

**Never block the read.** The tee appends each delta to a buffer, merging it with the one before when it continues the same item, and wakes a pump goroutine. The pump sends the buffered deltas to the run's events. A slow consumer makes deltas coarser; it never holds up the runner's read.

**Order before the final message.** `Respond` closes the request's stream before it returns: the pump sends what is left and stops. The coordinator records the response only after `Respond` returns, and the runner's `AssistantMessage` and `ReasoningSummary` come from that record, so every delta of a response reaches the session before its final events.

**The final message is authoritative.** The TUI keeps streamed text as ordinary transcript items marked `Streaming`. The runner's `AssistantMessage` replaces the oldest streamed message in place, and each `ReasoningSummary` the oldest streamed summary part, so any difference between the deltas and the recorded answer corrects itself. Streamed items no final event claimed are dropped when the run finishes.

**Drawing.** A streamed item is drawn exactly as a final message of its kind, with markdown, and redrawn on each change; the working line says `Writing` instead of `Thinking` while an answer streams. The TUI already reduces session events in 16 ms batches and draws one frame per batch (`internal/tui/bubble`), so a fast stream costs at most about 60 renders a second.

## Which requests stream

Only a session's own turn requests stream, and only when the session asks (`session.Options.Stream`, passed to the engine as `engine.Options.Stream`):

- The switcher (`adapter.go`), which the coordinator calls for each turn, puts the request's stream in its context; the transport tees only requests that carry one.
- A compaction summary and an auto-review call go through `switcher.direct()` and carry no stream.
- A subagent is a session of its own, and the agents package always opens it without `Stream`, so nothing streams into the parent's transcript or the subagent's view.
- The TUI's sessions and `uah run --stream` set `Stream`; plain `uah run` does not, since it prints the final answer only.

The process engine runs the runner as a subprocess and sees only its output, so it cannot stream: `Capabilities.Stream` is false there, and the capability table says the answer appears when the model finishes it.

## Retries and failures

- **A new attempt.** When the transport sees another attempt of a request that already streamed text, it sends `engine.StreamReset` before the attempt's first delta. The TUI drops the streamed items, so the failed attempt's text never mixes with the next one's.
- **A failed or canceled request.** When `Respond` returns an error after text streamed, the stream sends `StreamReset`: the runner records nothing for it. This covers a request the runner cancels because a message arrived, and the user's interrupt.
- **An in-band failure** (`response.failed`, an `error` event) arrives over a 2xx stream; the client's retry is a new attempt, as above.

## Out of scope

- Codex's newline-gated commits and smooth line-by-line animation: uah redraws the whole streamed item, which a 16 ms batch already bounds.
- Reasoning headings in the working line, as Codex's status header does: streamed summaries show only in the transcript when reasoning is shown (ctrl+r).
- Streaming tool-call arguments, such as an `apply_patch` as it is written.
- Streaming on the process engine, which would need the runner to print deltas.
- A configuration key to turn streaming off: nothing needed one.
