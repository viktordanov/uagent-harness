# Work ledger

The definitive list of work until it is done or the stop time arrives. Nothing outside this list gets built; an idea that comes up goes under [Later](#later), not into the work.

Stop time (first round): 07:50 local. Later rounds have none.

## Pending (round 3)

Asked for by the owner after item 28; these come first.

| # | Item | Lane | Status |
| --- | --- | --- | --- |
| 29 | MCP approvals from the CLI and the prompt; the guard hook becomes `forbid` rules | lane approvals | done |
| 30 | The auto-review prompt (and the compaction prompt) customizable, with a CLI that writes the defaults into the config folder as a starting point | lane approvals | done |
| 31 | Custom agents as Markdown files with front matter, as Claude Code and Codex have them; subagents never start subagents | lane agents3 | done |
| 32 | A real probe that forking a subagent reuses the provider's prompt cache | lane agents3 | done (partial reuse on openai-codex; see docs/design/subagents.md) |
| 33 | The process engine made solid, with the behavior both engines share in one place | lane process | done |
| 34 | On quit, print how to resume the session, as Codex does | main session | done |
| 35 | Research spike: Codex subscription usage (rate limits) on the openai-codex backend, isolated from the rest | lane usage | done (spike; see [docs/design/usage.md](design/usage.md)) |
| 36 | The composer's λ on its first row only | main session | done |
| 37 | Paste images into the prompt, as Codex and Claude Code do (ctrl+v on macOS; the Linux key to be found); first check what the runner and uagent allow | lane images | done (embedded engine; the image goes as a ViewImage result, since the runner's user message is text only; see docs/design/images.md) |
| 38 | `!` shell mode in the composer: run a command yourself, and its result joins the conversation | lane shell | done |
| 39 | Show the subscription's usage, as designed in item 35 (the owner accepted the defaults) | lane usage2 | done |

### 34. The resume hint on quit

When the TUI quits with a session open, it prints the command that continues it, as Codex prints "To continue this session, run codex resume <id>".

### 35. Codex usage (spike)

A research spike on how Codex reads the ChatGPT subscription's usage and rate limits (what it calls, what it shows, and when), whether uah can read the same through the openai-codex credentials, and a plan. Any code stays in its own package, wired to nothing, until the plan is accepted.

### 37. Pasting images

Codex and Claude Code let you paste an image from the clipboard into the prompt (ctrl+v on macOS, where cmd+v pastes text) and attach image files, and send it to the model with the message. First: how both do it (the keys on macOS and Linux, how they read the clipboard, how the image shows in the composer, what they send), and whether unreal-agent-runner and uagent can carry an image in a user message at all (the runner's inbox, `core.UserInput`, the session store, the Responses request). Then the plan, and the build if nothing upstream blocks it; a change the runner would need is written down, not made, since the runner stays unchanged.

- Built: ctrl+v and alt+v paste the clipboard's image on macOS (osascript) and Linux (wl-paste or xclip), as Codex's ctrl+v and alt+v; a pasted or dropped image path and an `@` image file attach too. `[Image #N]` placeholders show in the composer, one backspace removes one with its image, and the transcript shows them live and resumed. The image is stored in `<state>/images` and travels with the message as a tag line through the session, the engine, and the runner's inbox. The runner cannot put an image in a user message (`llm.Message` is text only, the context builder decodes only a string), so the embedded engine sends each image as a `ViewImage` call and result after the message; a probe on openai-codex confirmed the provider takes it. The process engine states the gap through the capability table. Open, defaults taken: the upstream runner change (image parts in `llm.Message`, a structured inbox payload, `input_image` in the Responses encoder) is written down, not made; no renumbering after a delete; no Windows or WSL clipboard reader; no store cleanup. See [the images design](design/images.md).

### 38. `!` shell mode

Typing `!` at the start of an empty composer switches it to shell mode: the λ becomes `!`, and enter runs the line as a command in the workspace (in the session's sandbox and permission mode) instead of sending it to the agent. The command and its output, whether it succeeded or failed, join the conversation as a message, so the agent sees them on its next turn, as Claude Code's `!` and Codex's user shell commands do. Backspace on an empty line, or esc, leaves shell mode. Research both first: how each shows it, what exactly goes into the conversation and when (at once, or with the next message), output limits, and whether a running agent is interrupted.

- Built, after Codex rust-v0.156.1 ([shell mode design](design/shell-mode.md)):
  - The composer's `!` mode, with the `!` prompt, the footer hint, and backspace or esc to leave.
  - `Session.RunShell` runs the command at once on both engines, also while the agent works, with streamed output, a one-hour timeout, and esc esc to stop it.
  - The record is Codex's `<user_shell_command>` message with the output cut to 40,000 characters. It goes with the next message, as `Inject` does, and never starts a turn.
  - A `KindShell` item shows the command with its output folded, also in a resumed session.
- Open, defaults taken:
  - The command runs outside the sandbox and the rules, as in Codex and Claude Code. `user_shell_sandbox = true` runs it in the permission mode's sandbox, and a `forbid` rule refuses it.
  - The record waits in memory for the next message, so quitting first loses it.
  - Claude Code's reply to the output (`respondToBashCommands`), its Ctrl+B background, and its `!` completion are not built.

### 39. Subscription usage

Build docs/design/usage.md's recommended design with its defaults, which the owner accepted: read the openai-codex plan's usage from `/wham/usage` (read-only, uah's own identity, on demand and after each run, cached for 60 s, no polling); `uah usage` (`--json`); `/status` rows with each window's percent left and reset time; the tightest window in the footer beside the context meter; warnings at 75, 90, and 95% used; "try again at …" when a run hits the limit; a `usage` check in `uah doctor`. Other providers say usage is not available.

- Built: `usage.Reader` (`For`, `CodexReader`: one request at a time, a 60 s cache for the read after each run, credentials loaded per read), built once per session in `app.Setup` and passed in `bubble.Deps.Usage`; `uah usage [--json]`; `/status` rows with a bar and a stale mark; the footer's `weekly 78% left`; warnings at 75, 90, and 95% used; "Usage limit reached; try again at …" from the failure text or a fresh read; the doctor's `usage` check. See [As built](design/usage.md#as-built).
- Open, default taken: the doctor check also fails when a limit is reached or the backend rejects the login, as the design's table says. The 429's exact text through the runner was not seen for real, so `usage.LimitReachedIn` accepts both forms the runner can produce. Option B (the headers) stays for later.

### 29. MCP approvals

- `uah mcp add <name> … --approve` sets `default_tools_approval_mode = "approve"` for the new server; `uah mcp approve <name> [tool] [--mode approve|prompt|writes|auto]` changes it later, through the comment-keeping editor (`internal/config/tomledit`).
- The approval prompt for an MCP call gets "Yes, and always allow this tool", which saves `tools.<tool>.approval_mode = "approve"` for that server in the user file, as "don't ask again" saves a rule for a command.
- This repository's `.uagent/config.toml` replaces the example guard hook with `[approvals] forbid` rules (the sandbox and the rules already cover what it blocked); the hook stays as an example in the hooks README only.
- Built: `uah mcp add --approve`, `uah mcp approve`, and "Yes, and don't ask again for this tool" (`a`), after Codex's "Allow and don't ask me again". Open, default taken: the approval is saved in the file that configures the server (the trusted project file when it has it, else the user file), as Codex does, because a user-file entry for a project server would be dropped by the merge.

### 30. Prompts you can customize

- The auto-reviewer's prompt, from a file (`[review] prompt_file`, or Codex's key if it has one), falling back to the built-in one.
- A CLI that writes the built-in prompts into the config folder as a starting point (for example `uah prompts init` writing `~/.config/uagent/prompts/review.md` and `compact.md`, and printing the keys that use them), so customizing starts from the real defaults. Compaction already reads `compact_prompt` and `experimental_compact_prompt_file`; the CLI covers it too.
- Built: `[review] policy_file`, `uah prompts init [--force]`, and `uah prompts show compact|review`. Open, default taken: Codex's key is `[auto_review] policy` (inline text that replaces only the policy inside the fixed template), so the file replaces the policy too, and the key is named after it; the framing and the output contract stay fixed because the verdict parser depends on them. Codex has no command that writes its built-in prompts out (its `/init` writes AGENTS.md), so the CLI stays this small.

### 31. Custom agents as Markdown

- Agent definitions as Markdown files with YAML front matter in `~/.config/uagent/agents/*.md` and a trusted workspace's `.uagent/agents/*.md`, as Claude Code's `.claude/agents/*.md` (name, description, tools, model) and beside Codex's TOML role files, which keep working. The body is the agent's instructions.
- Front matter can pre-approve: the tools the agent may use (an allow-list), and commands or MCP tools it may run without asking, within the parent's permission mode and never beyond it.
- Subagents never start subagents: the depth limit is fixed at 1, not configurable above it, and a child is never offered the spawn tools except as a fork keeping the parent's prefix, where every spawn is refused.

### 32. The fork, for real

One small real probe on openai-codex: a parent with some history spawns a child with `fork_context`, and the child's first response reports cached input tokens close to the parent's last request. The result goes into docs/design/subagents.md.

### 33. The process engine

- The behavior that does not depend on the engine lives in one place both engines use (instructions, hooks around the run, rules and permission modes where the runner allows, the session settings), so the process engine is not a second, thinner path.
- What the process engine cannot do (live input, approvals inside a run, compaction) is stated once, checked at session start, and reported in `uah doctor` and the TUI rather than failing later.
- Tests on the fake and the real runner for each of these.


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
| 14 | Shell completion: bash, zsh, fish, pwsh | main session | — | done |
| 15 | Module READMEs; root README as getting started, config surface, common tasks | main session + docs lane | 16–18 | done |
| 16 | Compaction validated for production | lane v-compaction | 4 | done |
| 17 | Subagents validated for production; children identical to the main agent except their nested session ID | lane v-subagents | 12 | done |
| 18 | MCP validated for production; `/mcp` view and OAuth login; `uah mcp` | lane v-mcp | 5 | done |
| 19 | The chosen TUI look | main session | 17, 18 | done |
| 20 | Diff rendering like Codex and Claude Code | lane diff | 19 | done |
| 21 | Model catalog from the provider, as Codex | lane models | — | done |
| 22 | Subagent handling: steer in the view, notifications to the parent, only working agents browsable, run IDs | main session | 17 | done |
| 23 | Quality pass over the new concepts | main session | 22 | done |
| 24 | Compaction you can configure, and a second look at how well it works | lane settings | 16 | done |
| 25 | `/config`: the basic settings in the TUI, saved to the user file | lane settings | 24 | done |
| 26 | Permission modes on shift+tab, shown in the TUI | lane modes | — | done |
| 27 | Session settings kept with the session: model, effort, fast mode, permission mode | lane modes | 26 | done |
| 28 | Tests for the process engine, codexauth, and the notification codec; a complexity backstop in CI | main session | 23 | done |

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
- Done: the runner has no edit tool, so the embedded engine offers Codex's `apply_patch` (function form, `input`), ported to `internal/patch` from Codex rust-v0.156.1. Writes inside the writable roots apply; others go through the approval pipeline under the run's live permission mode. The diff is computed at apply time, kept in the job's handle, and reaches the TUI as `engine.PatchApplied`, live and on reload. Open: the freeform (Lark grammar) form needs custom tools in the runner's Responses adapter; the approval overlay shows `apply_patch <paths>`, not the diff; a hard link inside the workspace to a file outside it is not caught (Codex runs the write in the sandbox for that).

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
- Done: Codex's `compact_prompt`, `experimental_compact_prompt_file`, and `model_auto_compact_token_limit`, and uah's `compact_model`, `compact_effort`, and `compact_user_message_max_tokens`; `/compact <focus>` as Claude Code's; `/context`'s buffer follows the effective limit. The second look fixed an automatic compaction that repeated before every request when it could not get under the limit, and the kept-message cap on small windows ([design](design/compaction.md#second-look-ledger-item-24)).

### 25. `/config`

Claude Code's `/config`, for the basic settings: auto-compact on or off and its percent, the compaction model, the default model and effort, fast mode, the permission mode, the details view, and the mouse. It shows each value and where it comes from (as `uah config` does), and a change is saved to the user file with the same editor `uah mcp add` uses, keeping comments.

Done: `internal/config/tomledit` is the one editor for both; the model, effort, fast mode, and permission mode also change the running session, the details view and the mouse change at once, and a change that would stop a session from starting is undone ([internal/tui](../internal/tui/README.md#config)).

### 26. Permission modes on shift+tab

shift+tab cycles three modes, shown in the footer (and the detailed view's header) and changed live:

| Mode | Sandbox | Escalations and `prompt` rules | Codex | Claude Code |
| --- | --- | --- | --- | --- |
| Read only | read-only | Ask (the auto-reviewer first) | `read-only` preset | `plan` (nearest) |
| Workspace (default) | workspace-write | Ask (the auto-reviewer first) | `auto` preset, reviewer user ("Ask for approval") | `default` |
| Auto | workspace-write | The auto-reviewer decides; you are not asked; a decline reaches the model with the reason | `auto` preset, reviewer auto_review ("Approve for me") | `auto` |

Full access (danger-full-access) stays a flag and a config value, outside the cycle, as Codex keeps Full Access out of its permission shortcut; shift+tab moves from it to Read only. The mapping and the Codex sources (approval presets, the permission shortcut, the turn-context override) are in the [approvals README](../internal/approval/README.md#permission-modes).

Built: `approval.Mode` (read-only, workspace, auto, full-access); `session.Settings.Mode`; `engine.Options.Mode`, `Capabilities.LiveMode`, and `Run.SetMode`. On the embedded engine a change applies from the next command (the Bash tool picks that mode's sandboxing shell) and the next model request (the switcher rewrites Bash's description of the sandbox); the ask reads it for each approval. On the process engine it applies from the next run (one sandboxing `SHELL` per mode), and the TUI says so. A subagent starts in its parent's mode at spawn. The `permission_mode` key picks a mode at start and wins over `sandbox_mode`; `--sandbox` still wins over both.

Open (defaults taken): no `--mode` flag, because Auto does not map onto `--sandbox` or `--ask` and a headless run cannot ask anyway, so `--sandbox` covers the rest; Full Access keeps `approval_policy` as configured instead of Codex's `never`; Claude Code's `acceptEdits` has no uah equivalent (the workspace sandbox already lets commands edit); when the reviewer's circuit breaker opens, Auto mode declines with the reason instead of asking the user as Claude Code's auto mode does after 3 blocks in a row or 20 in total.

### 27. Session settings kept with the session

The model (with its provider), effort, fast mode, and permission mode a session last used are saved in its sidecar (`sessions/<id>.uah.json`, `settings`) when it opens and whenever they change, and restored on resume ahead of the configured defaults; a flag still wins. Older sidecars without `settings` fall back to the last run's request for the provider, model, and effort, and to the configuration for fast mode and the mode. `uah config --session` shows `session` as their source. A subagent's sidecar keeps its own settings. The precedence in docs/configuration.md is pinned by a test that its resumed-session line names every saved key.

### 23. As done

The survey found 3 high, 11 medium, and 12 low findings. Fixed:

- High: an agent view that switched away left a goroutine blocked (it now drains until closed, and a view that fell behind reopens); subagent IDs showed as `subagent` in the detailed header and picker (one `session.ShortID`); tabs broke the band's width (expanded to four spaces).
- Medium: sidecars were read under the agents lock on every spawn and run start (a session's parent is remembered); two tree walks disagreed (one); updates went to the parent while holding a lock every child shared (each parent has an ordered outbox); closed children kept their event logs, and the log trim copied it on every event; `wait` counted down on a resumed child instead of the one it counted up; views were cached across reopenings and the main cache survived `/clear`; the notification format had two hand-written codecs (`engine.SubagentNotification` and `ParseSubagentNotification`); per-frame scans of the whole transcript for agents (`State.Agents`); a timing guess in an MCP test.
- Low: the dead window table; `/context`'s buffer uses `compaction.AutoLimit`; tool labels are shaped once in the state, not on every frame; `listAgents` uses the state's clock; `band` restores its background after any reset; a spawn that failed left a sidecar and record behind; the engine forgets a closed session's transcript, last request, fork, and cache key; SubagentStop hooks end with their child; two functions joined the documented complexity exceptions; the sleep behind a negative assertion; the flaky `TestTUI_CommandsAndPrompt`; tests for the view drain, `band`, and `ThemeFor`.

Deferred then and done after item 28: the model catalog is passed in instead of read from a process-wide default, and the theme's styles live in the render cache instead of package-level state.

### Second-round summary

Done: items 13–27. `/context`, shell completion, module READMEs and a root README as a guide; compaction, subagents, and MCP checked against Codex and hardened; the amber look; `/clear` in the same session; the model catalog from the provider; subagent notifications, steering, and the agent view; a quality pass; configurable compaction and `/config`; permission modes on shift+tab and settings kept per session; Codex's `apply_patch` with diffs. Open edges are under Later.

## Later

Ideas that come up while working go here, not into the items.

- Subagents: stopping a child while the parent is idle; Codex's v2 tools, `items`, and `fork_context`; Codex's completion notification into the parent's history (see docs/design/subagents.md, Validation).
- Auto-review: Codex sends only the transcript delta per review and lets the reviewer run read-only commands; uah sends the whole trimmed context each time. The openai API-key provider reviews with the session model (Codex uses gpt-5.6-luna).
- Approvals: "No, and tell the agent what to do" has no text field; no per-session cache of approved commands; the process engine ignores rules and approvals.
- MCP: resources, prompts, restarting a crashed server, applying `tools/list_changed`, reconnecting a server after `uah mcp login` without `/new` (docs/design/mcp.md, Open decisions).
- Compaction: the process engine cannot compact. On a switch to a model with a smaller window, Codex first compacts with the previous model (`turn.rs:1342`); uah compacts with the new one, which trims the oldest history. Claude Code's "Compact Instructions" in CLAUDE.md steer every summary; uah has `compact_prompt` only.
- `/config`: Claude Code's `/config key=value` form, a search field, and more rows (the sandbox's network access, the reviewer).
- Crash cleanup kills recorded process groups; a reused process group ID after a reboot could hit an unrelated process (uagent's end-of-run cleanup has the same risk).
- `uah config` does not list `[agents]` yet.

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

- 09:40 · 13 · `/context` like Claude Code: internal/contextusage, the engine records each session's last request, a 10×10 grid.
- 09:50 · 14–15 · Shell completion for bash, zsh, fish, and pwsh; module READMEs next to their code, and the root README as a guide.
- 11:20 · 16–18 · Merged the compaction, MCP (OAuth, `uah mcp`, `/mcp`), and subagents validation lanes, each checked against Codex.
- 11:45 · 19 · The amber look: transparent background, λ on a band, the tool column, the breathing λ, the finish line, the banner.
- 12:25 · 22 · `/clear` in the same session; subagents round 2 (fork_context, `subagent-` IDs, the agent view); the model catalog (21).
- 13:55 · 22 · Subagent notifications, steering the viewed agent, only working agents browsable; uagent v0.4.3 for run IDs.
- 14:20 · 23 · Quality pass: 24 of the survey's 26 findings fixed.
- 14:45 · 24–27, 20 · Merged configurable compaction and `/config`, permission modes and saved session settings, and `apply_patch` with diffs.
- 15:10 · 28 · A fresh review found CI red on a notification race (fixed: the note goes before the update), stale ledger lines (fixed), and no tests for the process engine (added), and suggested a complexity check (gocyclo at 20, as a backstop).
- 15:40 · 23 (deferred) · No process-wide model catalog: `compaction.ContextWindow` takes the catalog, and `app.Setup` returns the session's. No package-level theme: a `Styles` value in each render cache, with every drawing function its method (a go/types rewrite of 38 functions).

### Final summary (05:29)

Done: all twelve items. The quality pass; the sandbox finished with approvals, Codex rules, configured approvals, and auto-review on `codex-auto-review` (probed for real, ~3.5K input tokens and ~5 s per review); Codex-style compaction that keeps every user message with a context meter; MCP on the official SDK with Codex's config format and approval modes; the SQLite session index with search and the `/status` heatmap; the `/` and `@` menu; AGENTS.md and skills found as Codex finds them; hooks for PreCompact and PermissionRequest; the configuration reference with `uah config`; `uah doctor`, content-based hook trust, and a crash-recovery test whose fix went into uagent v0.4.2; and subagents with Codex's v1 tools, built beyond the research the item required.

Cut or unfinished: nothing on the list. The open edges each lane recorded are under Later.

First thing next (at the time): try it — `uah` in a repository, ask for work that needs an escalation and a subagent, and read `uah doctor`. The subagent edges named then (resume_agent, interrupts) were built in item 17.

