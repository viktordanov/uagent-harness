# uagent-harness

The general-purpose harness built on [uagent](https://github.com/viktordanov/uagent), the wrapper around unreal-agent-runner.
uagent runs one task with safety guards. This repository adds what long-lived, interactive work needs: sessions with a message queue and steering, an embedded engine for live model, effort, and fast-mode changes, instruction files, configuration, hooks, and a terminal UI.

Status: milestone M2 (sessions on the process engine). The TUI arrives in M4.

## Use it

```sh
go install github.com/viktordanov/uagent-harness/cmd/uah@latest

uah run -C ~/code/proj "Fix the failing test in pkg/foo"   # a session: progress on stderr, answers on stdout
uah sessions                                               # sessions, most recent first
uah sessions show 3f2a                                     # a transcript, by ID or unique prefix
uah run --session 3f2a "Now update the README"             # resume with the session's model, effort, and workspace
printf 'first\nsecond\n' | uah run --stdin                  # each line is a message; lines queue while the agent works
uah run --stream "..."                                     # JSONL: uagent's run events plus session events
```

`uah run` takes the same backend, guard, and state flags as uagent (`--provider`, `-m`, `-e`, `-t`, `-C`, `--state-dir`, `--runner`, `--max-disk`, `--allow-dotenv`); `uah run --help` lists them.
A flag wins over the environment (`UNREAL_HARNESS_LLM_*`, `UAGENT_*`), which wins over the resumed session's settings and the defaults. Sessions and run records live in uagent's state directory, so `uagent` and `uah` share them.

Design:

1. [Harness design](docs/design/harness.md): what the runner provides, what the harness adds, the two engines, and the accepted scope.
2. [TUI design](docs/design/tui.md): the framework choice, architecture, screens, keys, and commands.
3. [Implementation spec](docs/design/implementation.md): the packages and files in both repositories, types, milestones, and tests.
4. [TUI framework benchmark](bench/tui/README.md): the measurements behind choosing Bubble Tea v2 (a separate Go module).

## Development

Go 1.27.1 or later is required. Tests run the real process engine against uagent's fake runner and captured fixtures, so they need no model or tokens.

```sh
go run ./cmd/uah --version   # build and run uah
go test -race ./...          # unit and end-to-end tests
golangci-lint run ./...      # lint with .golangci.yml (golangci-lint v2.13.2)
```

CI (`.github/workflows/ci.yml`) runs the build, the race tests, and the linter on each push and pull request.
`bench/tui` is a separate Go module, so the root commands above do not include it.
