# uagent-harness

The general-purpose harness built on [uagent](https://github.com/viktordanov/uagent), the wrapper around unreal-agent-runner.
uagent runs one task with safety guards. This repository adds what long-lived, interactive work needs: sessions with a message queue and steering, an embedded engine for live model, effort, and fast-mode changes, instruction files, configuration, hooks, and a terminal UI.

Status: milestone M4: sessions, instructions, configuration, and the TUI, on the process engine. The embedded engine (live steering, live `/effort` and `/model`, `/fast`) comes next.

## Use it

```sh
go install github.com/viktordanov/uagent-harness/cmd/uah@latest

uah                                                        # the TUI: a live session in the current directory
uah -C ~/code/proj "Fix the failing test in pkg/foo"       # the TUI, starting with a prompt
uah --session 3f2a                                         # the TUI, resuming a session with its transcript

uah run -C ~/code/proj "Fix the failing test in pkg/foo"   # a session: progress on stderr, answers on stdout
uah sessions                                               # sessions, most recent first
uah sessions show 3f2a                                     # a transcript, by ID or unique prefix
uah run --session 3f2a "Now update the README"             # resume with the session's model, effort, and workspace
printf 'first\nsecond\n' | uah run --stdin                  # each line is a message; lines queue while the agent works
uah run --stream "..."                                     # JSONL: uagent's run events plus session events
```

### The TUI

| Key | Action |
| --- | --- |
| enter | Send. While the agent works, the message queues and goes out when the run ends |
| ctrl+enter (or alt+enter) | Send now: on the process engine this interrupts the run and restarts it with the queue and the message |
| shift+enter (or ctrl+j) | New line |
| esc esc | Interrupt the run; queued messages stay |
| ↑ on an empty composer | Take the last queued message back to edit it |
| alt+, / alt+. | Lower or raise the effort for the next run |
| ctrl+s, ctrl+n | Session picker, new session |
| ctrl+r | Show or hide reasoning summaries |
| pgup / pgdn | Scroll the transcript |
| ctrl+c | Clear the composer; on an empty composer, quit (twice while a run is live) |

Commands: `/model <id>`, `/effort <level>`, `/resume [id]`, `/new`, `/stop`, `/status`, `/reasoning`, `/help`, `/quit`. `/fast` needs the embedded engine.
Tool calls keep their place in the transcript, so a command that finishes after later turns updates its original row. Diagnostics go to `<state-dir>/logs/uah-tui.log`.

`uah run` takes the same backend, guard, and state flags as uagent (`--provider`, `-m`, `-e`, `-t`, `-C`, `--state-dir`, `--runner`, `--max-disk`, `--allow-dotenv`); `uah run --help` lists them.
A flag wins over the environment (`UNREAL_HARNESS_LLM_*`, `UAGENT_*`), which wins over the resumed session's settings and the defaults. Sessions and run records live in uagent's state directory, so `uagent` and `uah` share them.

### Instructions

The runner reads no instruction files, so `uah` builds them into the runner's system prompt, after the runner's own default text:

1. The user file: `~/.config/uagent/AGENTS.md`, or else `~/.codex/AGENTS.md`.
2. One file per directory from the repository root down to the workspace: `AGENTS.override.md`, else `AGENTS.md`, else `CLAUDE.md`.

Later files are more specific. The total stops at 32 KiB. `--no-instructions` turns this off, and the loaded files are reported as `instructions_loaded`.

### Configuration

`~/.config/uagent/config.toml` (or `$XDG_CONFIG_HOME/uagent/config.toml`, or `--config`) sets defaults below flags, the environment, and a resumed session:

```toml
provider = "openai-codex"
model = "gpt-6-sol"
effort = "high"
timeout = "30m"
max_disk = "5G"

[instructions]
enabled = true
max_bytes = 32768

# A workspace's .uagent/config.toml applies only when trusted here.
[projects."/Users/me/code/proj"]
trusted = true
```

Unknown keys are errors, so a typo fails loudly instead of being ignored.

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
