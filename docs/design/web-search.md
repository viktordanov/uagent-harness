# Web search

Ledger item 64: the model can search the web with the provider's hosted `web_search` tool, sees its past searches on later turns as in Codex, and the transcript shows what it searched. The runner stays unchanged: uah puts the searches the runner drops back into the request body, only by insertion.

Status: built, 2026-09-30, on the embedded engine.

1. [What Codex does](#what-codex-does)
2. [What the runner allows](#what-the-runner-allows)
3. [The design](#the-design)
4. [Putting searches back](#putting-searches-back)
5. [Sandbox and approvals](#sandbox-and-approvals)
6. [The probes](#the-probes)
7. [Limits](#limits)

## What Codex does

Checked against Codex rust-v0.156.1. Paths are under `codex-rs/`.

- **The key.** `web_search` is `disabled`, `cached`, `indexed`, or `live` (`app-server-protocol/schema/typescript/WebSearchMode.ts`). Unset, it is `cached` (`core/src/config/mod.rs:3697`), so search is on by default. The legacy features `web_search_cached` and `web_search_request` map to `cached` and `live`.
- **Per turn.** `resolve_web_search_mode_for_turn` (`core/src/config/mod.rs:3030`) keeps the configured mode, except that full access without a sandbox prefers `live`. A read-only turn still searches (`cached`), as the test `web_search_mode_updates_between_turns_with_permission_profile` shows (`core/tests/suite/web_search.rs`).
- **The tool.** `create_web_search_tool` (`core/src/tools/hosted_spec.rs`) sends `{"type":"web_search","external_web_access":<bool>}`: false for `cached`, true for `live` and `indexed` (which also sets `indexed_web_access`). `[tools.web_search]` adds `search_context_size`, `filters.allowed_domains`, and `user_location`, and models with `web_search_tool_type = text_and_image` get `search_content_types`.
- **Providers.** The hosted tool is offered only when the provider has the `web_search` capability and the model does not use Responses Lite (`core/src/tools/spec_plan.rs:593`). OpenAI and the ChatGPT backend have it; OSS providers do not; Bedrock gets `cached` only.
- **History.** `web_search_call` items stay in the conversation and are sent back on later requests (`core/src/context_manager/history.rs:878`), so the model sees its own searches. The item is sent as `type`, `id`, `status`, and `action` (`type` with `query` or `queries`, `url`, `pattern`) (`protocol/src/models.rs:1190`, `WebSearchAction` at `:1954`).
- **The TUI.** A search cell reads "Searching the web" while it runs and then "Searched <query>", "Opened <url>", or "Searched for '<pattern>' in <url>" (`tui/src/history_cell/search.rs`). The detail text is `web_search_action_detail` (`core/src/web_search.rs`): a query, the first of several queries with " ...", a URL, or a pattern in a URL.

## What the runner allows

Checked against unreal-agent-runner v0.1.1.

- `llm.Tool{Type: llm.ToolHosted, Name: "web_search"}` encodes as a bare `{"type":"web_search"}` (`harness/llm/responsesapi/request.go:322`). There is no field for `external_web_access`, filters, or content types, and any other hosted tool name is an error.
- The response parser drops `web_search_call` output items (`responsesapi/response.go:76`): they reach neither the coordinator nor the session file, so the next request has no trace of the search.
- The items the runner keeps go back with their provider IDs: a message's `id`, a function call's `id`, and a reasoning item verbatim (`responsesapi/request.go`). Each can anchor a search.
- The context builder adds any tool it is given (`contextbuilder.AddTool`), and the embedded engine builds it (`internal/engine/embedded/wiring.go`), so uah can add the hosted tool without the tool registry.

## The design

**The key.** `web_search = "live"` (the default) or `"disabled"` (`internal/app/websearch.go`). The API reads a bare `web_search` tool as live search, so live is the only on mode uah can send. `cached` and `indexed` are usage errors that say why, rather than a silent live. `uah config` and the `/config` panel show the key; a change applies to sessions opened next.

**Offering the tool.** `embedded.Config.WebSearch` turns it on, and `Provider.WebSearch` marks the providers that run it: `openai` and `openai-codex`. For each run, `hostedTools` (`internal/engine/embedded/websearch.go`) adds the hosted tool to the context builder after the registry's tools when both are true. Subagents run on the parent's engine, so they search too, as Codex's subagents do.

**Showing searches.** The runner drops the items, so the transport's tee reads them from the stream (`stream.go`, from item 45). Every turn request now carries a tee when the run has events; text deltas still stream only for runs that ask for them. The tee decodes `response.output_item.added` and `response.output_item.done` lines of type `web_search_call`, and skips other finished items unread, since they are large. A started search becomes `engine.WebSearch{Done: false}`, and a finished one `engine.WebSearch{Done: true}` with its action, query (or first query of several, as Codex shows it), URL, and pattern. The events go through the stream's pump, so they reach the session before the response's final events.

| Where | What it shows |
| --- | --- |
| TUI, compact view | `WEB` and `searching the web`, then `searched: <query>`, `opened: <url>`, or `searched: '<pattern>' in <url>` |
| TUI, detailed view | `✓ web_search  searched: <query>  hosted · <time>` |
| `uah exec` | a progress line: `searched: <query>` |
| `uah exec --json` | `{"type":"web_search","item_id":…,"done":…,"action":…,"query":…,"url":…,"pattern":…}` for the start and the end |

## Putting searches back

The first probe showed why the history matters: without the search item, the model later said it had not searched and had given its answer unchecked. Codex replays the items; the runner cannot. The owner chose to have uah put them back, without a runner change:

- **Record.** When searches are recorded (a provider with `Provider.WebSearch`), the tee also reads each `response.output_item.added` line: every output item's index, type, and ID. When the request succeeds, each finished search of the last attempt becomes a record: the item as Codex sends it (`type`, `id`, `status`, `action`), anchored `before` the next output item the runner keeps, or, when none followed it, `after` the one before it. Records go to `sessions/<id>.websearch.jsonl` (`internal/engine/embedded/searchlog.go`), so a resumed session has them; a failed or retried attempt records nothing.
- **Insert.** For a turn request, the transport (`watchTransport`) reads the body the runner wrote, finds the top-level `input` array's items and their IDs with a token decoder, and inserts each record's item before its anchor (or after it), in recorded order. The bytes are spliced in; everything else stays as the runner wrote it, byte for byte, so the prompt cache prefix is stable from one request to the next. A body without any anchor goes out unchanged.
- **Only turn requests.** The marker is the request's stream, which only the switcher's turn requests carry. A compaction summary and an auto-review call go through `switcher.direct()` and never get a search.
- **Dropping.** Nothing is ever removed from the log. A search applies only while its anchor is in the input: a compaction or `/clear` replaces the history with a summary or nothing, and a rewind cuts it, so the anchor and the search leave together.
- **Subagents.** A forked subagent's history holds its parent's items, so the fork copies the parent's log; a fresh subagent starts with none. `uah sessions rm` removes the log with the session.

Rewriting the body is a step past reading it, which is why it is limited to insertion at item boundaries of the one array the runner builds from history. If the runner ever keeps `web_search_call` items itself, this mechanism must go, or the model would see each search twice.

## Sandbox and approvals

The search runs on the provider's servers, not on the machine. The sandbox's network rule governs commands and does not apply, and no approval is asked, as in Codex, which offers search in read-only turns too. PreToolUse hooks do not see it: it is not a tool call the harness runs. Turn it off with `web_search = "disabled"`.

## The probes

On 2026-09-30, `uah exec` on openai-codex (gpt-6-sol, low effort) asked for the latest Go release, then, in a second message, whether it had searched:

1. Before the searches were put back: the ChatGPT backend accepted the bare tool, the stream carried one `web_search_call` (`action.type = "search"` with a `query` and `queries`), the tee reported its start and end, and the answer cited go.dev. The follow-up request, with the reasoning but without the dropped item, was accepted, but the model answered that it had not searched.
2. With the searches put back: the log held one record anchored before the answer's message ID, the backend accepted the second request with the item inserted, and the model answered that it had searched, naming both queries.

## Limits

- **Live search only.** No `cached` mode, no `[tools.web_search]` options, and no image results, since the runner sends only the tool type.
- **Live only in the transcript.** The session file has no record of a search, so a resumed transcript shows none; the model still gets them back from the log.
- **`/context`** estimates the request the runner built, without the inserted searches, which are small.
