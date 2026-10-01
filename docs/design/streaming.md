# Streaming the answer

Ledger item 45: the agent's answer, and its reasoning summaries when the TUI shows them, appear as the model writes them, not all at once when the response ends. The runner stays unchanged.

Status: built, 2026-09-25, on the embedded engine. Since ledger item 77 (2026-09-30), one observer per model request carries the stream, the retries, the timeouts, the progress, and the diagnostics.

1. [What Codex does](#what-codex-does)
2. [What the runner allows](#what-the-runner-allows)
3. [The design](#the-design)
4. [Which requests stream](#which-requests-stream)
5. [Retries and failures](#retries-and-failures)
6. [Timeouts and waiting for the network](#timeouts-and-waiting-for-the-network)
7. [Progress and diagnostics](#progress-and-diagnostics)
8. [Out of scope](#out-of-scope)

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
- The embedded engine builds each provider's client over its own `*http.Client` (`internal/engine/embedded/clients.go`, from item 41), whose transport sees every attempt (`transport.go`). The transport can wrap a response body and read the same bytes the runner reads, without changing them.

So the runner offers no hook, and none is needed: the transport sees the stream.

## The design

**One observer.** The switcher gives each model request a `modelCall` in its context (`internal/engine/embedded/modelcall.go`). The transport (`transport.go`) wraps each 2xx attempt's body in an `attemptBody`: the runner reads the base body's bytes unchanged, a line at a time, and each line first goes to one SSE scanner (`sse.go`). The scanner keeps only `data:` lines and decodes only the event types it needs; for the text:

| Event | Becomes |
| --- | --- |
| `response.output_text.delta` | `engine.TextDelta{ItemID, Text, Final}` |
| `response.reasoning_summary_text.delta` | `engine.ReasoningDelta{ItemID, Part, Text}` |
| `response.output_item.added` of a message | nothing; its `phase` sets `Final` on the item's deltas |

A finished item is decoded only when it is a web search or a tool call: the others, such as the final message, can be large.

**Never block the read.** The scanner appends each event to a buffer, merging it with the one before when it continues the same item, and wakes a pump goroutine. The pump sends the buffered events to the run's events. A slow consumer makes deltas coarser; it never holds up the runner's read.

**Order before the final message.** `Respond` ends the request's observer before it returns: the pump sends what is left and stops. The coordinator records the response only after `Respond` returns, and the runner's `AssistantMessage` and `ReasoningSummary` come from that record, so every delta of a response reaches the session before its final events.

**The final message is authoritative.** The TUI keeps streamed text as ordinary transcript items marked `Streaming`. The runner's `AssistantMessage` replaces the oldest streamed message in place, and each `ReasoningSummary` the oldest streamed summary part, so any difference between the deltas and the recorded answer corrects itself. Streamed items no final event claimed are dropped when the run finishes.

**Drawing.** A streamed item is drawn exactly as a final message of its kind, with markdown, and redrawn on each change; the working line says `Writing` instead of `Thinking` while an answer streams. The TUI already reduces session events in 16 ms batches and draws one frame per batch (`internal/tui/bubble`), so a fast stream costs at most about 60 renders a second.

## Which requests stream

Only a session's own turn requests stream, and only when the session asks (`session.Options.Stream`, passed to the engine as `engine.Options.Stream`):

- The switcher (`adapter.go`), which the coordinator calls for each turn, observes the request as a turn; only a turn's text streams.
- A compaction summary and an auto-review call go through `switcher.direct()`: their observer reports only retries.
- A subagent is a session of its own, and the agents package always opens it without `Stream`, so nothing streams into the parent's transcript or the subagent's view.
- The TUI's sessions and `uah run --stream` set `Stream`; plain `uah run` does not, since it prints the final answer only.
- Since item 64, every turn request of a run with events reports hosted web searches ([web search](web-search.md)); without `Stream`, it sends no text.

The process engine runs the runner as a subprocess and sees only its output, so it cannot stream: `Capabilities.Stream` is false there, and the capability table says the answer appears when the model finishes it.

## Retries and failures

- **A new attempt.** When the transport sees another attempt of a request that already streamed text, it sends `engine.StreamReset` before the attempt's first delta. The TUI drops the streamed items, so the failed attempt's text never mixes with the next one's.
- **A failed or canceled request.** When `Respond` returns an error after text streamed, the stream sends `StreamReset`: the runner records nothing for it. This covers a request the runner cancels because a message arrived, and the user's interrupt. The coordinator stops without waiting for the request it canceled, so the run waits for its requests in flight to end before it finishes: the reset comes before `RunFinished`, which drops events after it.
- **An in-band failure** (`response.failed`, an `error` event) arrives over a 2xx stream; the client's retry is a new attempt, as above.
- **Every retry is shown.** An attempt ends as the runner decides: a completed or incomplete response is the answer; an in-band failure is retried with the reason `code: message`; a stream that ends without its terminal event with "the stream ended before the response completed"; a read error or a status the client retries with that error or status. Each emits `engine.Reconnecting` with the next attempt and the runner's delay without its jitter (`retryDelay`: `Retry-After` in seconds or as a date, the "try again in" hint of a `rate_limit_exceeded`, 10 s doubling to 60 s for an overload, else the backoff), unless it was the last attempt or the request was canceled. Before item 77 a 2xx ended the tracking, so these retries ran under a plain "Thinking".

## Timeouts and waiting for the network

The runner's only limit was 30 minutes without a byte per attempt (`responsesapi/adapter.go`, v0.1.1), and uah's clients set no timeout, so a dead connection took about 5 minutes (TCP keepalive) and a silent peer 30 minutes. Now:

- **One transport** (`modelTransport`) for every client, the codex login's included, shared by an engine's runs and subagents per header timeout and closing its idle connections when the engine closes: a 2-minute response header timeout (none for a loopback server or Ollama), HTTP/2 pings (sent after 30 s quiet, 15 s to answer, 60 s per write), and TCP keepalive probes (30 s idle, every 10 s, 3 misses). Each fails as a timeout the runner retries.
- **Idle limit.** An attempt whose stream sends no `data:` line for 5 minutes, for any effort, is closed; the runner sees a net timeout, "no data from the model for 5m0s", and retries. Codex's stream idle timeout is the same 300 s.
- **Waiting for the network.** A dial that fails because a name does not resolve, there is no route (ENETUNREACH, EHOSTUNREACH, ENETDOWN, EADDRNOTAVAIL), or the dial timed out, waits inside the same attempt, 5 s doubling to 60 s ±10 %, as Codex's "Reconnecting... waiting for network", and emits `Reconnecting` with `Offline` and the reason "waiting for network: …". It never applies to a loopback server or a refused connection. No attempt is used up; the runner's 30 minutes bound the wait per attempt, and the attempt limit stays at 10.

## Progress and diagnostics

- **Phases.** A turn request emits `engine.ModelProgress`: connecting when the attempt starts, then sending (with the bytes sent), waiting, and streaming from `httptrace`, and done when it returns.
- **The tool call being written.** `response.output_item.added` of a function or custom tool call names it; each argument delta (`response.function_call_arguments.delta`, `response.custom_tool_call_input.delta`) adds to its size; for `apply_patch`, `patch.LastFile` finds the file the last header names in a 512-byte tail, JSON-escaped or not. The call's `output_item.done` clears it. Progress goes out at most every 200 ms while a call is written, else once a second while data flows; a queued progress replaces the one before. Direct calls report none.
- **Diagnostics.** Each attempt writes one JSON line to the run's `stderr.log` when its outcome is known (a retry when the next attempt starts): kind, attempt and limit, bytes sent and received, connect and first-byte times, status, events, the longest gap between data lines, the tool being written, the network wait, the result (ok, retry, failed, canceled), the reason, and the delay.

## Out of scope

- Codex's newline-gated commits and smooth line-by-line animation: uah redraws the whole streamed item, which a 16 ms batch already bounds.
- Reasoning headings in the working line, as Codex's status header does: streamed summaries show only in the transcript when reasoning is shown (`/reasoning`).
- Streaming tool-call arguments into the transcript: only their size and an `apply_patch`'s file reach the status line.
- Streaming on the process engine, which would need the runner to print deltas.
- A configuration key to turn streaming off: nothing needed one.
