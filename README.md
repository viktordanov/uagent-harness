# uagent-harness

The general-purpose harness built on [uagent](https://github.com/viktordanov/uagent), the wrapper around unreal-agent-runner.
uagent runs one task with safety guards. This repository adds what long-lived, interactive work needs: sessions with a message queue and steering, an embedded engine for live model, effort, and fast-mode changes, instruction files, configuration, hooks, and a terminal UI.

Status: milestone M6: sessions, instructions, configuration, the TUI, the embedded engine, and hooks. Sandboxing and approvals are researched in [docs/design/sandbox-research.md](docs/design/sandbox-research.md) but not built.

## Use it

```sh
go install github.com/viktordanov/uagent-harness/cmd/uah@latest

uah                                                        # the TUI: a live session in the current directory
uah -C ~/code/proj "Fix the failing test in pkg/foo"       # the TUI, starting with a prompt
uah resume                                                 # pick a session of this directory to resume (--all: any directory); uah run sessions are hidden, as Codex hides exec sessions
uah resume --last                                          # resume this directory's most recent session
uah --session 3f2a                                         # the TUI, resuming a session with its transcript
uah --fast                                                 # priority processing (openai and openai-codex)

uah run -C ~/code/proj "Fix the failing test in pkg/foo"   # a session: progress on stderr, answers on stdout
uah sessions                                               # this directory's sessions, most recent first (--all: every directory)
uah sessions show 3f2a                                     # a transcript, by ID or unique prefix
uah run --session 3f2a "Now update the README"             # resume with the session's model, effort, and workspace
uah run --last -m gpt-6-luna "And the changelog"           # resume this directory's latest session with another model
printf 'first\nsecond\n' | uah run --stdin                  # each line is a message; lines queue while the agent works
uah run --stream "..."                                     # JSONL: uagent's run events plus session events
```

### The TUI

The default view is compact, like Codex: your messages, one line per command (`• Ran go test ./...`), and the answers. ctrl+t (or `/details`) switches to the detailed view with the header, run dividers, turns with token counts, and session totals; `[tui] details = true` starts there.

| Key | Action |
| --- | --- |
| enter | Send. While the agent works, the message queues and goes out when the run ends |
| ctrl+enter (or alt+enter) | Send now: on the embedded engine the running agent reads it before its next model request; on the process engine the run restarts with the queue and the message |
| shift+enter (or ctrl+j) | New line |
| esc esc | Interrupt the run; queued messages stay |
| ↑ on an empty composer | Take the last queued message back to edit it |
| alt+, / alt+. | Lower or raise the effort for the next run |
| ctrl+s, ctrl+n | Session picker, new session |
| ctrl+t | Compact or detailed view |
| ctrl+r | Show or hide reasoning summaries |
| pgup / pgdn | Scroll the transcript |
| ctrl+c | Clear the composer; on an empty composer, quit (twice while a run is live) |

Commands: `/model <id>`, `/effort <level>`, `/resume [id]`, `/new`, `/stop`, `/status`, `/details`, `/reasoning`, `/help`, `/quit`. `/model`, `/effort`, and `/fast` apply from the next model request on the embedded engine, and from the next run on the process engine. `/fast` needs the embedded engine and the openai or openai-codex provider.
Tool calls keep their place in the transcript, so a command that finishes after later turns updates its original row. Diagnostics go to `<state-dir>/logs/uah-tui.log`.

`uah run` takes the same backend, guard, and state flags as uagent (`--provider`, `-m`, `-e`, `-t`, `-C`, `--state-dir`, `--runner`, `--max-disk`, `--allow-dotenv`); `uah run --help` lists them.
A flag wins over the environment (`UNREAL_HARNESS_LLM_*`, `UAGENT_*`), which wins over the resumed session's settings and the defaults. Sessions and run records live in uagent's state directory, so `uagent` and `uah` share them.

### Engines

`uah` runs the agent in one of two ways, chosen with `--engine`, `UAH_ENGINE`, or `engine` in the configuration:

- **embedded** (the default): the runner's own packages (unreal-agent v0.1.1) run inside `uah`, wired as the runner wires them. Messages, effort, model, and `--fast` reach a running agent. An interrupt is a hard stop through the runner's inbox, so the session file records the stopped tools. No `unreal-agent-runner` binary is needed.
- **process**: `uah` spawns `unreal-agent-runner` through uagent. The runner reads its request once, so messages sent while it works wait for the next run.

Both engines share uagent's guards, session lock, and run records, and write the same session files, so a session can move between them. The workspace `.env` is never loaded by the embedded engine.

### Instructions

The runner reads no instruction files, so `uah` builds them into the runner's system prompt, after the runner's own default text:

1. The user file: `~/.config/uagent/AGENTS.md`, or else `~/.codex/AGENTS.md`.
2. One file per directory from the repository root down to the workspace: `AGENTS.override.md`, else `AGENTS.md`, else `CLAUDE.md`.

Later files are more specific. The total stops at 32 KiB. `--no-instructions` turns this off, and the loaded files are reported as `instructions_loaded`.

### Hooks

Hooks run a command at a session event, with Claude Code's contract: the event arrives as JSON on stdin, exit 0 continues (optionally printing JSON), exit 2 blocks with stderr as the reason, and any other exit is reported and ignored.

```toml
[[hooks.PreToolUse]]            # embedded engine only
matcher = "Bash"                # a regular expression on the tool name
command = "~/.config/uagent/hooks/no-rm-rf.sh"
timeout = "10s"                 # default 60s

[[hooks.Stop]]
command = "osascript -e 'display notification \"uah is idle\"'"
```

| Event | When | What a hook can do |
| --- | --- | --- |
| `SessionStart` | The session opens (`source`: startup or resume) | Add context to the first message (`additionalContext` or plain stdout), show a `systemMessage` |
| `UserPromptSubmit` | Before a message is sent | Block it (exit 2 or `"decision":"block"`), or add context (`additionalContext` or plain stdout) |
| `PreToolUse` | Before each tool call, on the embedded engine | Deny it (exit 2 or `permissionDecision: "deny"`); the reason is the tool's error result. Rewrite it (`updatedInput`) |
| `PostToolUse` | After each tool call | Observe only |
| `Stop` | The agent finished and nothing is queued | Keep it going: `"decision":"block"` with a `reason` sends the reason as the next message (at most 5 times in a row; `stop_hook_active` is true after the first) |
| `SessionEnd` | The session closes | Observe only, with at most a second |

Hooks in the user file run as written. Hooks in a trusted project's `.uagent/config.toml` run only after `uah hooks trust` records their exact commands (by SHA-256, in `~/.config/uagent/trusted-hooks.json`); a changed command needs trust again. `uah hooks` lists the hooks for a workspace and whether each runs. Hook runs appear in the TUI's detailed view (ctrl+t); blocks and failures appear in both views.

### Configuration

`~/.config/uagent/config.toml` (or `$XDG_CONFIG_HOME/uagent/config.toml`, or `--config`) sets defaults below flags, the environment, and a resumed session:

```toml
provider = "openai-codex"
model = "gpt-6-sol"
effort = "high"
timeout = "30m"
max_disk = "5G"
engine = "embedded"   # or "process"
fast = false          # priority processing

[instructions]
enabled = true
max_bytes = 32768

[tui]
details = false   # start in the detailed view

# A workspace's .uagent/config.toml applies only when trusted here.
[projects."/Users/me/code/proj"]
trusted = true
```

Unknown keys are errors, so a typo fails loudly instead of being ignored.

Design:

1. [Harness design](docs/design/harness.md): what the runner provides, what the harness adds, the two engines, and the accepted scope.
2. [TUI design](docs/design/tui.md): the framework choice, architecture, screens, keys, and commands.
3. [Implementation spec](docs/design/implementation.md): the packages and files in both repositories, types, milestones, and tests.
4. [State storage](docs/design/state.md): what is stored where today, and the plan for a rebuildable SQLite index.
5. [Sandboxing and approvals](docs/design/sandbox-research.md): how Codex sandboxes and approves commands, bubblewrap, and options for uah (research, not decided).
6. [TUI framework benchmark](bench/tui/README.md): the measurements behind choosing Bubble Tea v2 (a separate Go module).

## Development

Go 1.27.1 or later is required. Tests need no model or tokens:

- The process engine runs against uagent's fake runner and captured fixtures.
- The embedded engine runs against `testing/fakellm`, a scripted Responses API. One test drives the real `unreal-agent-runner` (built from go.mod's version) and the embedded engine with the same script, and requires the same events and session items. `go test -short` skips it.

```sh
go run ./cmd/uah --version   # build and run uah
go test -race ./...          # unit and end-to-end tests
golangci-lint run ./...      # lint with .golangci.yml (golangci-lint v2.13.2)
```

CI (`.github/workflows/ci.yml`) runs the build, the race tests, and the linter on each push and pull request.
`bench/tui` is a separate Go module, so the root commands above do not include it.
