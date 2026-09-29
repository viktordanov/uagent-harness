# Web search

Ledger item 64: the model can search the web with the provider's hosted `web_search` tool, and the transcript shows what it searched. The runner stays unchanged.

Status: built, 2026-09-30, on the embedded engine.

1. [What Codex does](#what-codex-does)
2. [What the runner allows](#what-the-runner-allows)
3. [The design](#the-design)
4. [Sandbox and approvals](#sandbox-and-approvals)
5. [The probe](#the-probe)
6. [Limits](#limits)

## What Codex does

Checked against Codex rust-v0.156.1. Paths are under `codex-rs/`.

- **The key.** `web_search` is `disabled`, `cached`, `indexed`, or `live` (`app-server-protocol/schema/typescript/WebSearchMode.ts`). Unset, it is `cached` (`core/src/config/mod.rs:3697`), so search is on by default. The legacy features `web_search_cached` and `web_search_request` map to `cached` and `live`.
- **Per turn.** `resolve_web_search_mode_for_turn` (`core/src/config/mod.rs:3030`) keeps the configured mode, except that full access without a sandbox prefers `live`. A read-only turn still searches (`cached`), as the test `web_search_mode_updates_between_turns_with_permission_profile` shows (`core/tests/suite/web_search.rs`).
- **The tool.** `create_web_search_tool` (`core/src/tools/hosted_spec.rs`) sends `{"type":"web_search","external_web_access":<bool>}`: false for `cached`, true for `live` and `indexed` (which also sets `indexed_web_access`). `[tools.web_search]` adds `search_context_size`, `filters.allowed_domains`, and `user_location`, and models with `web_search_tool_type = text_and_image` get `search_content_types`.
- **Providers.** The hosted tool is offered only when the provider has the `web_search` capability and the model does not use Responses Lite (`core/src/tools/spec_plan.rs:593`). OpenAI and the ChatGPT backend have it; OSS providers do not; Bedrock gets `cached` only.
- **History.** `web_search_call` items stay in the conversation and are sent back on later requests (`core/src/context_manager/history.rs:878`), so the model sees its own searches.
- **The TUI.** A search cell reads "Searching the web" while it runs and then "Searched <query>", "Opened <url>", or "Searched for '<pattern>' in <url>" (`tui/src/history_cell/search.rs`). The detail text is `web_search_action_detail` (`core/src/web_search.rs`): a query, the first of several queries with " ...", a URL, or a pattern in a URL.

## What the runner allows

Checked against unreal-agent-runner v0.1.1.

- `llm.Tool{Type: llm.ToolHosted, Name: "web_search"}` encodes as a bare `{"type":"web_search"}` (`harness/llm/responsesapi/request.go:322`). There is no field for `external_web_access`, filters, or content types, and any other hosted tool name is an error.
- The response parser drops `web_search_call` output items (`responsesapi/response.go:76`): they reach neither the coordinator nor the session file.
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

## Sandbox and approvals

The search runs on the provider's servers, not on the machine. The sandbox's network rule governs commands and does not apply, and no approval is asked, as in Codex, which offers search in read-only turns too. PreToolUse hooks do not see it: it is not a tool call the harness runs. Turn it off with `web_search = "disabled"`.

## The probe

On 2026-09-30, `uah exec` on openai-codex (gpt-6-sol, low effort, an ephemeral session) asked for the latest Go release:

- The ChatGPT backend accepted the bare tool. The stream carried one `web_search_call` (`action.type = "search"` with a `query`), the tee reported its start and end, and the answer cited go.dev.
- A second message in the same session was accepted too: the request without the dropped item, but with the reasoning before it, is valid.
- Asked what it had searched for, the model answered that it had not searched and had given the version without checking it. The next request has no trace of the search. See [Limits](#limits).

## Limits

- **The model does not see its past searches.** Codex sends `web_search_call` items back; the runner drops them. Within a turn the results are in use; on later turns the model sees only its reasoning and its answer, and may disown what it found. Fixing it needs the items back in the history: a runner change, or uah reinserting them in the request body.
- **Live search only.** No `cached` mode, no `[tools.web_search]` options, and no image results, since the runner sends only the tool type.
- **Live only in the transcript.** The session file has no record of a search, so a resumed transcript shows none.
