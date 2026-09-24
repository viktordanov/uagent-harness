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
| 2 | Sandbox phase 2: approvals, rules, configured approvals | A | 1 | doing |
| 3 | Sandbox phase 3: auto-review and the one-shot model call | A | 2 | todo |
| 4 | Compaction, Codex's way, and the context meter | B | 1 | doing |
| 5 | MCP | C | 1 | doing |
| 6 | Session storage index and the `/status` activity heatmap | D | 1 | done |
| 7 | `/` menu and `@` mentions | D | 1 | done |
| 8 | AGENTS.md and skills parity with Codex | B | 4 | done |
| 9 | Hooks for the new features | main session | 2, 4, 5 | todo |
| 10 | Configuration reference and `uah config` | main session | 2, 5 | todo |
| 11 | `uah doctor`, content-based hook trust, crash-recovery test | C | 5 | todo |
| 12 | Subagents: research and plan | any free lane | — | done |

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

## Later

Ideas that come up while working go here, not into the items.

## Log

One line per merge or decision: time, item, what landed, commit.

- 04:30 · 1 · CI lints for linux and darwin (matrix); go mod tidy; audit recorded in architecture.md (files ≤ 400 lines; the reducer and printer switches kept as decision tables). No other findings: the earlier split of session, app, and embedded already fixed the grab-bags.
- 04:30 · 12 · docs/design/subagents.md: Codex v1 tool set (spawn_agent, send_input, wait, close_agent), [agents] config and role files, children as uah sessions; build after items 2–5.
- 04:36 · 6 · internal/store: SQLite index (modernc.org/sqlite, WAL, FTS5) reconciled from run records, equal to the file scan by test; listing, --last, and the picker use it with a file-scan fallback; `uah sessions --search`; `/status` 12-week heatmap. Context threaded through app.Setup and FindSession.
- 04:40 · 7 · The composer menu (internal/tui/state/menu.go): commands and values after `/`, fuzzy workspace files after `@` (sahilm/fuzzy; git ls-files, else a walk); tab fills, ↑/↓ move, enter runs, esc closes. Fixed item 6: the shell did not route ActivityLoaded, so the heatmap never showed; a shell-level test covers it now.
- 04:44 · 8 · AGENTS.md discovery with Codex keys (project_doc_fallback_filenames, none by default so CLAUDE.md is opt-in; project_root_markers; project_doc_max_bytes; blank files skipped); skills from Codex places (.agents/skills up to the project root, $CODEX_HOME/skills, ~/.config/uagent/skills) through the runner's SkillUse. The owner's config keeps CLAUDE.md via the fallback key.
