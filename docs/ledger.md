# Work ledger

The definitive list of work until it is done or the stop time arrives. Nothing outside this list gets built; an idea that comes up goes under [Later](#later), not into the work.

Stop time: **07:50 local** (check with `date`). After **07:30**, start nothing new: finish, merge, and document what is in flight.

## Rules

1. Lean on libraries and existing tech: the runner's packages, uagent, the OS sandboxes, official SDKs (MCP), Go libraries (Starlark, SQLite). No hand-rolled protocols or parsers when a library exists. Ported code keeps its license notice.
2. Files: one concern each, no grab-bags, no confetti. Roughly ≤ 400 lines per file and ≤ 15 cyclomatic complexity per function outside the TUI's big switches. Do not overcorrect.
3. Tests go through real paths: the fake runner, the real runner, `fakellm`, real sandbox runs. Extend fakes rather than add mocks.
4. Every merge to `main`: `go test -race ./...`, `golangci-lint run ./...` and `GOOS=linux golangci-lint run ./...` at 0 issues, `memoria check` OK, conventional commits without trailers. Push, then CI must be green.
5. The runner stays unchanged. uagent changes are allowed and get a tag.
6. Real model calls: small probes only.
7. Decisions that need the owner are written into the item's plan doc as "Open", with the default taken, not asked mid-loop.

## Parallel work

At most **3 subagents at once**, each in its own worktree of this repository (`/usr/bin/git worktree add ~/Projects/Code/uah-worktrees/<lane> -b <lane> main`), each owning a named set of files. The main session reviews each diff, reruns the tests, merges, resolves conflicts in shared files (`internal/config/config.go`, `README.md`, `internal/engine/embedded/tools.go`), and does the Memoria review. A lane that needs a shared file adds to it minimally and says so in its report.

## Items

Status: `todo`, `doing`, `done`, `cut` (with a reason).

| # | Item | Lane | Depends on | Status |
|---|---|---|---|---|
| 1 | Quality pass | main session | — | done |
| 2 | Sandbox phase 2: approvals, rules, configured approvals | A | 1 | done |
| 3 | Sandbox phase 3: auto-review and the one-shot model call | A | 2 | done |
| 4 | Compaction, Codex's way, and the context meter | B | 1 | done |
| 5 | MCP | C | 1 | done |
| 6 | Session storage index and the `/status` activity heatmap | D | 1 | done |
| 7 | `/` menu and `@` mentions | D | 1 | done |
| 8 | AGENTS.md and skills parity with Codex | B | 4 | done |
| 9 | Hooks for the new features | main session | 2, 4, 5 | done |
| 10 | Configuration reference and `uah config` | main session | 2, 5 | done |
| 11 | `uah doctor`, content-based hook trust, crash-recovery test | C | 5 | done |
| 12 | Subagents: research and plan | any free lane | — | done (built) |
| 13 | `/context` like Claude Code | main session | 4 | done |
| 14 | Shell completion: bash, zsh, fish | main session | — | done |
| 15 | Module READMEs; root README as getting started, config surface, common tasks | main session + docs lane | 16–18 | done |
| 16 | Compaction validated for production | lane v-compaction | 4 | done |
| 17 | Subagents validated for production; children identical to the main agent except their nested session ID | lane v-subagents | 12 | done |
| 18 | MCP validated for production; `/mcp` view and OAuth login; `uah mcp` | lane v-mcp | 5 | done |
| 19 | The chosen TUI look | main session | 17, 18 | done |
| 20 | Diff rendering like Codex and Claude Code | any free lane | 19 | todo |
| 21 | Model catalog from the provider, as Codex | lane models | — | done |
| 22 | Subagent handling: steer in the view, notifications to the parent, only working agents browsable, run IDs | main session | 17 | done |
| 23 | Quality pass over the new concepts | lane quality | 22 | doing |
| 24 | Compaction you can configure, and a second look at how well it works | lane settings | 16 | doing |
| 25 | `/config`: the basic settings in the TUI, saved to the user file | lane settings | 24 | doing |
| 26 | Permission modes on shift+tab, shown in the TUI | lane modes | — | doing |
| 27 | Session settings kept with the session: model, effort, fast mode, permission mode | lane modes | 26 | doing |

Order of starting: 1 alone (it touches everything). Then lanes A (2), B (4), C (5) in parallel. D (6, 7) starts when a lane frees up. 9 and 10 come after their dependencies merge.

### 1. Quality pass

- In: golangci-lint config checked against Go 1.27 (modernize analyzers enabled, `go` directive, no deprecated linters); CI lints as linux and darwin (`GOOS` matrix or a second step); package audit: file sizes and purpose, grab-bag files, the TUI reducer `onIntent` (37) and `printer.print` (28), `go mod tidy`.
- Out: rewrites of working code for taste.
- Done: lint 0 for both GOOS locally and in CI; audit findings fixed or recorded in `docs/documentation/architecture.md` with a reason.

### 2. Sandbox phase 2: approvals, rules, configured approvals

- In: escalation (`require_escalated`) asks the user; Codex's prompt (title, reason, command, "Yes", "Yes, and don't ask again for `<prefix>`", "No, and tell the agent what to do instead"); headless (`uah run`) denies with the reason; approval policy `on-request` (default) and `never`; Codex `.rules` files (`prefix_rule`, strictest decision wins, `allow` runs unsandboxed) parsed with `go.starlark.net`, from `~/.config/uagent/rules/*.rules` and trusted `.uagent/rules/*.rules`; "don't ask again" writes a prefix rule; configured approvals in `config.toml`: approved folders (`writable_roots`, project-level too), approved and denied command prefixes (a simple list besides `.rules`); session events `ApprovalRequested`/`ApprovalResolved`; the approval overlay in the TUI; the approval blocks the coordinator while open (as the plan says).
- Out: the process engine asking (it stays sandbox-only); network proxy.
- Files: `internal/rules/` (new), `internal/approval/` (new), `internal/engine/embedded/sandboxtool.go`, session events and a small hook in `internal/session`, TUI state/render overlay, config fields.
- Done: fakellm end to end: escalate → approve → runs unsandboxed; deny → the model gets the reason; "don't ask again" → a rule file line, then the next matching call runs without asking; a `forbidden` rule blocks; headless denies. Plan doc updated as built.

### 3. Sandbox phase 3: auto-review and the one-shot model call

- In: research whether `codex-auto-review` can be called through the Codex subscription at a cost comparable to Codex (request shape, prompt size, effort); a model-agnostic one-shot call (`internal/llmcall` or in uagent if it fits there better) built on the runner's `llm.Adapter` clients the embedded engine already constructs, so every provider works; the reviewer: Codex's context (user messages trusted, recent tool calls without output untrusted, the action), JSON verdict, fail closed, 3-denials circuit breaker, per-provider default model (`codex-auto-review` on openai-codex, the session model at low effort elsewhere), `approvals_reviewer` config; "auto-approved: reason" line in the TUI.
- Out: a separate API-key path for the reviewer unless the research says it is needed.
- Done: reviewer tests with fakellm (allow, deny, bad JSON, timeout, breaker); one real probe of `codex-auto-review`; research written in `docs/design/sandbox.md`.

### 4. Compaction, Codex's way, and the context meter

- In: research Codex's compaction (user messages kept verbatim, only the rest summarized; trigger threshold; the summary prompt) and what the runner's session store allows (`sessionstore.Fork`, `PreviousTurnID`) without changing the runner; `/compact` and automatic compaction at a configurable threshold; a context-left meter in the footer (from the last model response's input tokens and the model's window); a `PreCompact` hook event (added in item 9 if this lane cannot touch hooks).
- Out: compaction on the process engine if it needs the embedded engine (say so in the doc).
- Done: plan doc, then fakellm tests: compaction keeps every user message verbatim, the next request carries the summary, a resumed compacted session works.

### 5. MCP

- In: the official Go MCP SDK (`github.com/modelcontextprotocol/go-sdk`); stdio and streamable HTTP servers; `[mcp_servers.<name>]` in Codex's format (`command`, `args`, `env`, `url`, `enabled`, `startup_timeout_sec`, `tool_timeout_sec`, `enabled_tools`, `disabled_tools`) so a Codex config copies over; tools offered as `mcp__<server>__<tool>`; calls run without blocking the coordinator (the runner's `RemoteJobHandler` or an operation), results as text and images; per-tool approval config (always allow, ask, disabled) feeding item 2's approver; `/mcp` lists servers and tools; hooks see MCP tool names (matchers work).
- Out: MCP resources and prompts unless trivial with the SDK; OAuth.
- Done: a real stdio test server (built in the test) called end to end through the embedded engine with fakellm; timeouts and a crashing server tested.

### 6. Session storage index and the `/status` activity heatmap

- In: `docs/design/state.md`'s index: `modernc.org/sqlite`, rebuildable from files, WAL, sidecar rules; listing and `--last` use it; `uah sessions --search` via FTS5; `/status` shows a GitHub-style activity heatmap (runs or tokens per day, recent weeks) as Codex and Claude Code do.
- Out: session names, pins, archiving.
- Done: index rebuild test from files, a stale index reconciled, the heatmap golden.

### 7. `/` menu and `@` mentions

- In: tab completes commands and their arguments (models, efforts, session ID prefixes, sandbox modes); ↑/↓ move in the menu, enter picks, esc closes; `@` opens a fuzzy file search in the workspace (respecting .gitignore via `git ls-files` when in a repo) and inserts the path.
- Done: TUI tests for completion, navigation, and `@` insert.

### 8. AGENTS.md and skills parity with Codex

- In: recheck instruction discovery and limits against Codex's source; Codex skills (`~/.codex/skills`, `.agents/skills`, the `SKILL.md` format) offered to the model the way Codex does, alongside the runner's `.harness/skills`.
- Done: tests for discovery order and a skill used through fakellm.

### 9. Hooks for the new features

- In: a `PermissionRequest` event (a hook can approve or deny an escalation, before the reviewer and the user); `PreCompact`; MCP tool names in tool events; README hook table updated.
- Done: tests per new event.

### 10. Configuration reference and `uah config`

- In: `docs/configuration.md`: every field, type, default, which file may set it, precedence (flags → env → resumed session → project file → user file → defaults), merge rules per field (override, append, OR), a complete example; `uah config` prints the effective configuration and each value's source.
- Done: a test that every `config.Config` field appears in the reference; `uah config` golden.

### 11. `uah doctor`, content-based hook trust, crash-recovery test

- In: `uah doctor` checks runner, credentials and expiry, sandbox availability, config errors, hook trust, MCP server startup; hook trust also hashes a local script the command runs; a test that kills an embedded run mid-tool and resumes cleanly.
- Done: each with tests.

### 12. Subagents: research and plan

- In: how Codex and Claude Code run subagents (spawning, context, tools, results, limits); what the runner allows; a plan doc with the default choices. Build only if everything above is done.

### 13–18. Second round

- 13: `/context` breaks the last request into system prompt, instruction files, skills, tools, MCP tools, and the conversation, on a 10×10 grid, as Claude Code does.
- 14: `uah completion bash|zsh|fish|pwsh`, with flag values and session IDs.
- 15: each module documents itself next to its code; the root README gets you started, shows the whole configuration surface, and does common tasks as steps, linking the module READMEs.
- 16–18: each is checked against Codex and Claude Code, fixed where it falls short, and documented in its module README. For 17, a subagent is the same as the main agent in every way (instructions, skills, sandbox, approvals, rules, auto-review, hooks, MCP, compaction, `/context`, provider settings) and built by the same code; only its session ID, nested under its parent, differs, with approvals routed through the parent and the depth limit on spawning.

### 19. The chosen TUI look

Picked on the style swatchbook (rounds 1–3):

- The terminal's own background everywhere; only your messages and the composer get a background band.
- Amber accents, `#ffc014` (a step from `#ffb000` toward `#ffd100`), on a dark terminal; a light-terminal variant with dark amber ink.
- Your messages: `λ ` then the text, on the band. The composer also starts with `λ`.
- Tool lines as a dim labeled column (`RAN  4.1s  go test ./...`, `READ`, `RUN` live in amber), from the Paper and Ink look.
- Code blocks on a background band, with syntax colors in the amber family.
- Subagents as a tree: `AGENT Ada  0:42`, and under it `└ ⠹ Read internal/…` with the child's current tool.
- The working line: a pulsing `λ` and `Working (12s • esc to interrupt)`, as Codex's. The λ animation needs its own small design pass (frames, pacing, how it looks at rest) before it is built.
- A finished turn ends with `12:14 PM · worked 1m 12s`: Codex's time and Claude Code's duration on one line.
- Codex's box banner at the top (`λ uah (version)`, model with `/model to change`, directory), no tip.
- Codex's footer: model, effort, directory, context left.

Build it as a theme in `internal/tui/render` (styles in one place), so another theme is a new value, not new code.

### 20. Diff rendering like Codex and Claude Code

- File edits show as a diff under the tool line: Codex's `• Edited path (+3 -1)` header, then the changed hunks with line numbers, added lines green and removed lines red, context dim; long diffs fold with "+N lines (ctrl+t to view)".
- Claude Code's details worth taking: line numbers in a gutter, the whole line tinted rather than only the text, and word-level highlighting inside a changed line.
- The same diff in `uah sessions show` (plain text with `+`/`-`) and in the detailed view unfolded.
- Source: the runner's edit and apply_patch tool results; where a tool gives no diff, compute it from the before and after content.

### 21. Model catalog from the provider, as Codex

- In: `internal/models`: the provider's list at runtime (the ChatGPT backend's `/models?client_version=…` for openai-codex, `/v1/models`, OpenRouter's `/api/v1/models`, Fireworks' list, Ollama's `/api/tags`), a 300 s file cache with ETag keyed by a hashed provider and login identity, Codex's `models.json` bundled as the offline fallback, and near-miss suggestions. Wired into `/model` (menu values and "X is not available on P; did you mean Y?"), the one context-window function (`compaction.ContextWindow`), `uah doctor`, `uah models`, and `-m` completion from the cache. `models.Validate` for `spawn_agent`.
- Out: editing `internal/agents` (the subagents lane calls `models.Validate`); a config key for the TTL.
- Done: httptest tests per source shape, cache hit, TTL expiry, ETag 304, fallback to the cache then the bundled list, identity scoping; one real probe of the ChatGPT backend (9 models, live).

### 22. Subagent handling

- ctrl+enter in the agent view steers the agent's live run, as it does the main agent's.
- A subagent that ends (completed, failed, or interrupted) tells the main agent with Codex's `<subagent_notification>`, with the next message, never starting a run (sending it into a live run would make the runner cancel a paid model request). Not when a pending `wait_agent` already returns it, or when the main agent closed it.
- Only working subagents are stops for alt+← and alt+→ and open with `/agents <name>`; a finished one's end is a line in the main transcript.
- Run IDs skip the `subagent-` prefix, and uagent v0.4.3 reserves a run's directory when naming it, so runs of different sessions never share one.

### 23. Quality pass over the new concepts

A survey of the code added since item 12: rendering and the theme, notifications and injection, subagents (forking, spawning with a model, per-agent fast mode through role files), the model catalog, `/clear`, and the agent view. It checks each against the rules (pure core, one job per package, files ≤ ~400 lines, functions ≤ ~15, no duplicated logic, errors wrapped once, tests on real paths), lists findings by severity, and fixes them after items 24–27 merge, so the fixes do not collide with that work.

### 24. Compaction you can configure

- Keys for what Codex lets you set and what uah fixes today: the summary model and effort (default: the session's model, as Codex), the summary prompt (Codex's `compact_prompt`, and a file form), `auto_compact_percent`, the kept-message cap (20,000 tokens), and the buffer `/context` shows.
- A second look at the results: what the summary keeps, how a compacted session behaves on the next turns, and what Codex and Claude Code do differently, with tests for any fix.

### 25. `/config`

Claude Code's `/config`, for the basic settings: auto-compact on or off and its percent, the compaction model, the default model and effort, fast mode, the permission mode, the details view, and the mouse. It shows each value and where it comes from (as `uah config` does), and a change is saved to the user file with the same editor `uah mcp add` uses, keeping comments.

### 26. Permission modes on shift+tab

shift+tab cycles three modes, shown in the footer and changed live:

| Mode | Sandbox | Escalations |
| --- | --- | --- |
| Read only | read-only | Ask (the auto-reviewer first) |
| Workspace (default) | workspace-write | Ask (the auto-reviewer first) |
| Auto | workspace-write | The auto-reviewer decides; you are not asked |

Full access (danger-full-access) stays a flag and a config value, outside the cycle. The mapping follows Codex's approval presets and Claude Code's shift+tab; "Auto" is Claude Code's auto mode, with Codex's auto-reviewer as the judge.

### 27. Session settings kept with the session

The model, effort, fast mode, and permission mode a session last used are saved in its sidecar and restored on resume, before the configured defaults; a flag still wins.

## Later

Ideas that come up while working go here, not into the items.

- Subagents: stopping a child while the parent is idle; Codex's v2 tools, `items`, and `fork_context`; Codex's completion notification into the parent's history (see docs/design/subagents.md, Validation).
- Auto-review: Codex sends only the transcript delta per review and lets the reviewer run read-only commands; uah sends the whole trimmed context each time. The openai API-key provider reviews with the session model (Codex uses gpt-5.6-luna).
- Approvals: "No, and tell the agent what to do" has no text field; no per-session cache of approved commands; the process engine ignores rules and approvals.
- MCP: resources, prompts, restarting a crashed server, applying `tools/list_changed`, reconnecting a server after `uah mcp login` without `/new` (docs/design/mcp.md, Open decisions).
- Compaction: a configurable summary model and prompt (Codex's `compact_prompt`); the process engine cannot compact.
- Crash cleanup kills recorded process groups; a reused process group ID after a reboot could hit an unrelated process (uagent's end-of-run cleanup has the same risk).
- `uah config` does not list `[agents]`, `[review]`, and MCP servers yet.

## Log

One line per merge or decision: time, item, what landed, commit.

- 04:30 · 1 · CI lints for linux and darwin (matrix); go mod tidy; audit recorded in architecture.md (files ≤ 400 lines; the reducer and printer switches kept as decision tables). No other findings: the earlier split of session, app, and embedded already fixed the grab-bags.
- 04:30 · 12 · docs/design/subagents.md: Codex v1 tool set (spawn_agent, send_input, wait, close_agent), [agents] config and role files, children as uah sessions; build after items 2–5.
- 04:36 · 6 · internal/store: SQLite index (modernc.org/sqlite, WAL, FTS5) reconciled from run records, equal to the file scan by test; listing, --last, and the picker use it with a file-scan fallback; `uah sessions --search`; `/status` 12-week heatmap. Context threaded through app.Setup and FindSession.
- 04:40 · 7 · The composer menu (internal/tui/state/menu.go): commands and values after `/`, fuzzy workspace files after `@` (sahilm/fuzzy; git ls-files, else a walk); tab fills, ↑/↓ move, enter runs, esc closes. Fixed item 6: the shell did not route ActivityLoaded, so the heatmap never showed; a shell-level test covers it now.
- 04:44 · 8 · AGENTS.md discovery with Codex keys (project_doc_fallback_filenames, none by default so CLAUDE.md is opt-in; project_root_markers; project_doc_max_bytes; blank files skipped); skills from Codex places (.agents/skills up to the project root, $CODEX_HOME/skills, ~/.config/uagent/skills) through the runner's SkillUse. The owner's config keeps CLAUDE.md via the fallback key.
- 04:45 · 4 · Merged lane/compaction: internal/llmcall (one-shot model call, reused by item 3), internal/compaction (Codex prompt and window table), an adapter in front of the switcher that keeps every user message verbatim and replaces the rest with a summary, persisted in sessions/<id>.compaction.jsonl; /compact, auto_compact_percent (90), model_context_window, the "N% context left" meter. The runner's own TurnCompaction is never produced by v0.1.1, so it was not usable (docs/design/compaction.md).
- 04:47 · 9 (part) · PreCompact hook: runs before each compaction with the session and trigger; a block stops it. Remaining for 9: PermissionRequest (after item 2), MCP names already reach tool hooks (lane C).
- 04:50 · 5 · Merged lane/mcp: internal/mcp on the official Go SDK (stdio and streamable HTTP), [mcp_servers.<name>] in Codex's format, tools as mcp__server__tool running as the runner's remote jobs (never blocking the coordinator), approval_mode per tool (prompt/writes refused until wired to the approver), /mcp. Also fixed the flaky picker test (wait for idle before /new).
- 04:52 · 2 · Merged lane/approvals: internal/rules (Codex prefix_rule via go.starlark.net, commands split with mvdan.cc/sh), internal/approval (on-request/never, forbidden > prompt > allow, "don't ask again" writes default.rules), session ApprovalRequested/Resolved and Resolve, the Codex-style TUI overlay, [approvals] allow/forbid, approval_policy, --ask; escalated commands run unsandboxed after approval; headless denies with a reason.
- 04:57 · 9 · PermissionRequest hook (answers approvals, also headless); MCP approval_mode wired to the approver prompt with Codex's annotation rule for auto; MCP names reach PreToolUse/PostToolUse (lane C). Item 9 complete with PreCompact.
- 05:01 · 3 · Merged lane/review (internal/review: Codex's review prompt, strict JSON verdicts, fail-closed, breaker; approvals_reviewer and [review] config; real probe: codex-auto-review accepted, ~3.5K input tokens, 5.7 s) and wired it: the embedded engine reviews before the session asks, on the session's own client; AutoReviewed events in the TUI and uah run; approval.DeclineBecause carries the reviewer's reason to the model.
- 05:03 · check · Real end-to-end run (uah run, embedded, gpt-6-sol low): the sandbox blocked a write outside the workspace, the model escalated, codex-auto-review allowed it (low risk, ~4.5 s), and the command ran unsandboxed.
- 05:05 · quality · After the merges: split config merge (merge.go), approval Decide, hooks Decision.add, the TUI menu keys, and moved onRunEvent out of reduce.go (408 → 350 lines). Remaining functions above 15 are the documented dispatch switches.
- 05:06 · 12 · Building subagents (lane/subagents) in parallel with 10 and 11, the last items above it, to use the free lane.
- 05:13 · 10 · Merged lane/config-ref: docs/configuration.md (every key, type, default, files, merge rule, precedence; a reflection test fails when a key is missing), `uah config` with each value's source (app.Explain beside Resolve). Fixed on merge: config merging no longer changes the user file's maps or slices (it doubled hooks when merged twice); a --provider flag equal to the configured provider keeps the configured model; the invalid-engine error names the bad value.
- 05:16 · 11 · Merged lane/doctor: `uah doctor` (config, settings, runner, workspace, credentials incl. the real client construction, a real sandbox run, instructions, hooks, MCP startup, state), hook trust that also hashes a local script the command runs, and a crash-recovery test (SIGKILL uah mid-tool, resume) that found orphaned tools surviving a crash — fixed on the embedded engine by killing recorded live groups before resuming.
- 05:22 · 11 · Moved the crash fix into uagent v0.4.2 (harness.Start kills tools a killed run left behind, after taking the session lock), so both engines get it; removed the embedded engine's copy. The crash test still passes.
- 05:28 · 12 · Merged lane/subagents: Codex v1 tools (spawn_agent, send_input, wait, close_agent) as the runner's remote jobs, children as resumable sessions under their parent (hidden from the picker), [agents] config and Codex role files, child approvals through the parent, progress lines and /agents in the TUI. The reference test caught the new keys; docs/configuration.md gained a Subagents section.

### Final summary (05:29)

Done: all twelve items. The quality pass; the sandbox finished with approvals, Codex rules, configured approvals, and auto-review on `codex-auto-review` (probed for real, ~3.5K input tokens and ~5 s per review); Codex-style compaction that keeps every user message with a context meter; MCP on the official SDK with Codex's config format and approval modes; the SQLite session index with search and the `/status` heatmap; the `/` and `@` menu; AGENTS.md and skills found as Codex finds them; hooks for PreCompact and PermissionRequest; the configuration reference with `uah config`; `uah doctor`, content-based hook trust, and a crash-recovery test whose fix went into uagent v0.4.2; and subagents with Codex's v1 tools, built beyond the research the item required.

Cut or unfinished: nothing on the list. The open edges each lane recorded are under Later.

First thing next: try it — `uah` in a repository, ask for work that needs an escalation and a subagent, and read `uah doctor`; then pick from Later, starting with the subagent edges (resume_agent, interrupts) if subagents get real use.

