# uagent-harness architecture

uah is a pure core with well-organized infrastructure around it, not layered DDD.

| Package | Role |
| --- | --- |
| `internal/session` | The long-lived session: one goroutine owns settings, the queue, the live run, hooks, and one ordered event stream. |
| `internal/engine` | How runs execute: `process` spawns the runner through uagent; `embedded` runs the runner's packages in process as a uagent `harness.Backend`. |
| `internal/instructions` | AGENTS.md discovery and the host prompt, following Codex. |
| `internal/config` | TOML configuration: the user file and trusted project files. |
| `internal/hooks` | Hook contract, execution, and the trust store (commands, and the content of a local script a command runs). |
| `internal/mcp` | MCP servers in Codex's configuration format: starting them, naming their tools, and calling them, through the official Go SDK. |
| `internal/sandbox` | Sandbox policies and the sandboxing shell: Seatbelt on macOS, bubblewrap on Linux. |
| `internal/rules` | Codex's `.rules` files (Starlark `prefix_rule`) and command splitting for matching. |
| `internal/approval` | The approver: rules and the approval policy decide whether a command runs sandboxed, unsandboxed, or not, and ask the user through the session. |
| `internal/review` | The auto-reviewer: one model call over `internal/llmcall` judges an action that needs approval, with Codex's prompt, a fail-closed verdict, and a circuit breaker. No engine wiring. |
| `internal/tui/state` | The pure TUI model: a reducer from events and intents to state and effects. No I/O. |
| `internal/tui/render` | Pure drawing of state to lines, with a per-item cache. |
| `internal/tui/bubble` | The Bubble Tea shell: keys to intents, effects to commands, and frames. |
| `internal/app` | Session setup: `Resolve` picks settings from flags, the resumed session, the configuration, and defaults with no I/O; `Setup` loads files and builds the engine; `Doctor` runs the same steps as checks for `uah doctor`. |
| `cmd/uah` | The CLI: flags, `run`, `resume`, `sessions`, `hooks`, `doctor`, and the TUI launcher. |
| `testing` | `harnesstest` (fake and real runners, isolated state), `fakellm` (a scripted Responses API), and `mcpserver` (a stdio MCP server). |

## Rules

- The runner stays unchanged. uah reproduces its wiring instead of patching it, and the equivalence test compares both engines.
- `internal/tui/state` and `internal/tui/render` do no I/O; effects are values the shell runs.
- Session state is owned by the session goroutine; other goroutines talk to it through messages.
- Files are the source of truth for state: the runner's session files, uagent's run records, and uah's sidecars (see `docs/design/state.md`).
- Wrap errors with `fmt.Errorf("failed to <action>: %w", err)`, and log with `slog` to stderr or the TUI log file.
- Test through real code paths: the fake runner, the real runner built from go.mod, and `fakellm`, instead of mocks.

## Size and complexity

Files stay under about 400 lines with one concern each, and functions under about 15 cyclomatic complexity. Two known exceptions are switches over closed sets, where splitting would scatter one decision table:

- `(*State).onIntent` in `internal/tui/state/reduce.go` maps each intent to its state change.
- `(*printer).print` in `cmd/uah/print.go` maps each event to one line of progress.

Lint runs for linux and darwin (CI matrix; locally `GOOS=linux golangci-lint run ./...`), because the sandbox has build-tagged halves.
