# uah architecture

uah is a pure core with well-organized infrastructure around it, not layered DDD.

| Package | Role |
| --- | --- |
| `internal/session` | The long-lived session: one goroutine owns settings, the queue, the live run, hooks, and one ordered event stream. |
| `internal/sessionfile` | The runner's session file as a documented, versioned format, read by those rules with no runner code: the header, items by `Sequence`, paging, and the last item. |
| `internal/engine` | How runs execute: `embedded` runs the runner's packages in process as a uagent `harness.Backend`. |
| `internal/instructions` | AGENTS.md discovery and the host prompt, following Codex. |
| `internal/config` | TOML configuration: the user file and trusted project files. |
| `internal/home` | uah's home, `~/.uah` or `$UAH_HOME`, where every file uah reads and writes lives; `migrate` copies the folders earlier versions used into it once, at startup. |
| `internal/hooks` | Hook contract, execution, and the trust store (commands, and the content of a local script a command runs). |
| `internal/mcp` | MCP servers in Codex's configuration format, through the official Go SDK: starting and watching them, naming and calling their tools, OAuth logins and their storage, and editing `[mcp_servers]` in the user file. Its own README describes it. |
| `internal/sandbox` | Sandbox policies and the sandboxing shell: Seatbelt on macOS, bubblewrap on Linux. |
| `internal/rules` | Codex's `.rules` files (Starlark `prefix_rule`) and command splitting for matching. |
| `internal/approval` | The approver: rules and the approval policy decide whether a command runs sandboxed, unsandboxed, or not, and ask the user through the session. |
| `internal/history` | The prompt history file, `<home>/history.jsonl`, in Codex's format: appends under a lock, the size cap, and reading it back for the TUI's ↑ and ctrl+r. |
| `internal/usershell` | A command the user types in the TUI's `!` shell mode: running it (outside the sandbox and the rules unless `user_shell_sandbox`), its bounded output, and Codex's `<user_shell_command>` record the agent sees. The session runs it and holds the record for the next message. |
| `internal/agents` | Subagents behind the `engine.Subagents` seam: Codex's v1 tools, child sessions on the parent's engine, their limits, depth, approvals through the parent, SubagentStop hooks, resume, and Codex role files. The engine only offers the tools and runs their calls; see the package README. |
| `internal/compaction` | Compaction the Codex way: the request rewrite, the summary call over any `llm.Adapter`, token estimates, the window table, and the compaction log. The engine decides when to compact. |
| `internal/llmcall` | One model call outside the agent loop over any runner `llm.Adapter`, for summaries and reviews. |
| `internal/review` | The auto-reviewer: one model call over `internal/llmcall` judges an action that needs approval, with Codex's prompt, a fail-closed verdict, and a circuit breaker. No engine wiring. |
| `internal/codereview` | Codex's `/review`: the targets, the reviewer's prompts and rubric, and the findings it answers with; the session runs the reviewer through `internal/agents`. |
| `internal/gitdiff` | Read-only git for `/diff` and `/review`: the work tree's changes as display diffs, the branches, the recent commits, and a merge base. |
| `internal/cmdparse` | What a shell command does, for the TUI's tool lines: a port of Codex's `parse_command` (reads, listings, searches), and uah's own wrapper stripping, relative paths, heredoc folding, and summary line. Pure. |
| `internal/tui/state` | The pure TUI model: a reducer from events and intents to state and effects. No I/O. |
| `internal/tui/render` | Pure drawing of state to lines, with a per-item cache. |
| `internal/tui/bubble` | The Bubble Tea shell: keys to intents, effects to commands, and frames. |
| `internal/app` | Session setup: `Resolve` picks settings from flags, the resumed session, the configuration, and defaults with no I/O; `Explain` reports each effective value and its source the same way; `Setup` loads files and builds the engine; `Doctor` runs the same steps as checks for `uah doctor`. |
| `cmd/uah` | The CLI: flags, `exec` (also `run`), `resume`, `sessions`, `hooks`, `config`, `doctor`, `mcp`, and the TUI launcher. |
| `testing` | `harnesstest` (fake and real runners, `RunnerEngine` that spawns either for tests, isolated state, `IsolatedMain` for a package's `TestMain`), `fakellm` (a scripted Responses API), `mcpserver` (a stdio MCP server), and `oauthserver` (an MCP server behind a small OAuth authorization server). |

## Rules

- The runner stays unchanged. uah reproduces its wiring instead of patching it, and the equivalence test compares the embedded engine with the real runner.
- `internal/tui/state` and `internal/tui/render` do no I/O; effects are values the shell runs.
- Session state is owned by the session goroutine; other goroutines talk to it through messages.
- Files are the source of truth for state: the runner's session files, uagent's run records, and uah's sidecars (see `docs/design/state.md`).
- Wrap errors with `fmt.Errorf("failed to <action>: %w", err)`, and log with `slog` to stderr or the TUI log file.
- Test through real code paths: the fake runner, the real runner built from go.mod, and `fakellm`, instead of mocks.
- No test reads the user's `~/.uah`. A package whose tests can reach uah's home (`internal/home`, directly or through `internal/config` and `internal/app`) runs them through `harnesstest.IsolatedMain`, which sets `UAH_HOME` to a temporary directory and clears `UAH_CONFIG`, `UAH_STATE_DIR`, and `UAH_EXTRA_CONFIG`; `cmd/uah`'s `TestMain` does the same for the binary it builds.

## Size and complexity

Files stay under about 400 lines with one concern each, and functions under about 15 cyclomatic complexity. golangci-lint's `gocyclo` fails at 20 as a backstop, so only a runaway function stops CI; each exception above 20 carries a `nolint:gocyclo` comment pointing here. The known exceptions are dispatch switches over closed sets, where splitting would scatter one decision table:

- the TUI reducer's `onIntent`, `onEvent`, `onRunEvent`, and the menu's key handler `onMenu` (`internal/tui/state`), the patch parser's `updateLine` state machine (`internal/patch`, ported from Codex), the shell's `Update`, `onKey`, and effect runner `run` (`internal/tui/bubble`), and `itemLines` (`internal/tui/render`);
- `(*printer).print` in `cmd/uah/print.go`, one line of progress per event;
- the session's `loop` (`internal/session/loop.go`), one case per command and internal event.

Lint runs for linux and darwin (CI matrix; locally `GOOS=linux golangci-lint run ./...`), because the sandbox has build-tagged halves.
