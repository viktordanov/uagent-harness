<!-- memoria:section id="overview" files="cmd/uah/main.go go.mod" -->
# uagent-harness

The general-purpose harness built on [uagent](https://github.com/viktordanov/uagent), the wrapper around unreal-agent-runner.
uagent runs one task with safety guards. This repository adds what long-lived, interactive work needs: sessions with a message queue and steering, an embedded engine for live model, effort, and fast-mode changes, instruction files, configuration, hooks, and a terminal UI.

Status: milestone M6: sessions, instructions, configuration, the TUI, the embedded engine, and hooks. Sandboxing and approvals are planned in [docs/design/sandbox.md](docs/design/sandbox.md), following Codex, but not built yet.

1. [Use it](#use-it): the TUI, engines, instructions, hooks, and configuration
2. [Development](#development)
<!-- /memoria:section -->

<!-- memoria:section id="usage" files="cmd/uah/main.go cmd/uah/run.go cmd/uah/resume.go cmd/uah/sessions.go cmd/uah/tui.go cmd/uah/print.go internal/session/history.go internal/session/sidecar.go internal/store/store.go internal/store/query.go" -->
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
uah sessions --search "flaky parser"                       # sessions whose prompts or answers contain the words
uah sessions show 3f2a                                     # a transcript, by ID or unique prefix
uah run --session 3f2a "Now update the README"             # resume with the session's model, effort, and workspace
uah run --last -m gpt-6-luna "And the changelog"           # resume this directory's latest session with another model
printf 'first\nsecond\n' | uah run --stdin                  # each line is a message; lines queue while the agent works
uah run --stream "..."                                     # JSONL: uagent's run events plus session events
```

<!-- /memoria:section -->

<!-- memoria:section id="tui" files="internal/tui/state/commands.go internal/tui/state/reduce.go internal/tui/bubble/keys.go internal/tui/bubble/model.go internal/tui/render/items.go internal/tui/render/screen.go internal/tui/render/markdown.go" -->
### The TUI

The default view is compact, like Codex: your messages, one line per command (`• Ran go test ./...`), and the answers, with Markdown drawn as Codex draws it (highlighted code blocks, `code`, bold, headings, lists). ctrl+t (or `/details`) switches to the detailed view with the header, run dividers, turns with token counts, and session totals; `[tui] details = true` starts there.

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
| mouse wheel, shift+↑ / shift+↓, pgup / pgdn | Scroll the transcript; end returns to the bottom. While the TUI reports the mouse, select text with Option (iTerm2, Terminal) or Shift (most others) held |
| ctrl+c | Clear the composer; on an empty composer, quit (twice while a run is live) |

Commands: `/model <id>`, `/effort <level>`, `/resume [id]`, `/new`, `/stop`, `/status` (with a 12-week activity heatmap), `/details`, `/reasoning`, `/help`, `/quit`. `/model`, `/effort`, and `/fast` apply from the next model request on the embedded engine, and from the next run on the process engine. `/fast` needs the embedded engine and the openai or openai-codex provider.
Tool calls keep their place in the transcript, so a command that finishes after later turns updates its original row. Diagnostics go to `<state-dir>/logs/uah-tui.log`.

`uah run` takes the same backend, guard, and state flags as uagent (`--provider`, `-m`, `-e`, `-t`, `-C`, `--state-dir`, `--runner`, `--max-disk`, `--allow-dotenv`); `uah run --help` lists them.
A flag wins over the environment (`UNREAL_HARNESS_LLM_*`, `UAGENT_*`), which wins over the resumed session's settings and the defaults. Sessions and run records live in uagent's state directory, so `uagent` and `uah` share them.

<!-- /memoria:section -->

<!-- memoria:section id="engines" files="internal/engine/engine.go internal/engine/embedded/engine.go internal/engine/embedded/wiring.go internal/engine/embedded/client.go internal/engine/embedded/store.go internal/engine/embedded/agent.go internal/engine/embedded/providers.go internal/engine/process/process.go internal/session/dispatch.go internal/session/runs.go internal/app/resolve.go internal/app/setup.go" -->
### Engines

`uah` runs the agent in one of two ways, chosen with `--engine`, `UAH_ENGINE`, or `engine` in the configuration:

- **embedded** (the default): the runner's own packages (unreal-agent v0.1.1) run inside `uah`, wired as the runner wires them. Messages, effort, model, and `--fast` reach a running agent. An interrupt is a hard stop through the runner's inbox, so the session file records the stopped tools. No `unreal-agent-runner` binary is needed.
- **process**: `uah` spawns `unreal-agent-runner` through uagent. The runner reads its request once, so messages sent while it works wait for the next run.

Both engines share uagent's guards, session lock, and run records, and write the same session files, so a session can move between them. The workspace `.env` is never loaded by the embedded engine.

<!-- /memoria:section -->

<!-- memoria:section id="instructions" files="internal/instructions/instructions.go" -->
### Instructions

The runner reads no instruction files, so `uah` builds them into the runner's system prompt, after the runner's own default text:

1. The user file: `~/.config/uagent/AGENTS.md`, or else `~/.codex/AGENTS.md`.
2. One file per directory from the repository root down to the workspace: `AGENTS.override.md`, else `AGENTS.md`, else `CLAUDE.md`.

Later files are more specific. The total stops at 32 KiB. `--no-instructions` turns this off, and the loaded files are reported as `instructions_loaded`.

<!-- /memoria:section -->

<!-- memoria:section id="sandbox" files="internal/sandbox/sandbox.go internal/sandbox/shell.go internal/sandbox/seatbelt.go internal/sandbox/bwrap.go internal/sandbox/denied.go internal/sandbox/env.go internal/engine/embedded/sandboxtool.go internal/engine/embedded/tools.go internal/app/setup.go internal/app/resolve.go" -->
### Sandbox

Commands run in the operating system's sandbox, as in Codex: Seatbelt (`sandbox-exec`) on macOS and bubblewrap (`bwrap`, which must be installed) on Linux. The mode comes from `--sandbox`, `UAH_SANDBOX`, or `sandbox_mode`:

| Mode | Commands can |
| --- | --- |
| `workspace-write` (default) | Read any file; write the workspace, `/tmp`, `$TMPDIR`, and `writable_roots`, except `.git`, `.uagent`, `.agents`, and `.codex`; no network unless `network_access = true` |
| `read-only` | Read any file; write nothing; no network |
| `danger-full-access` | Anything your user can: no sandbox |

On the embedded engine the model can ask to run a command outside the sandbox (`sandbox_permissions: "require_escalated"` with a `justification`). This version refuses those requests with a reason; approvals, rules, and auto-review come next ([plan](docs/design/sandbox.md)). When a command fails in a way that looks like the sandbox blocked it, the model is told so. On the process engine, the runner's `SHELL` is a script that sandboxes each command. Where no sandbox is available, uah says so and runs commands without one. On Linux, a protected name that does not exist yet (such as `.git` in a workspace that is not a repository root) is not protected, because bubblewrap can only cover existing paths; macOS protects it either way.

Commands get the whole environment, as in Codex. `[shell_environment_policy]` narrows it with Codex's keys: `inherit` (`all`, `core`, `none`), `ignore_default_excludes = false` to drop names matching `*KEY*`, `*SECRET*`, `*TOKEN*`, `exclude` and `include_only` patterns, and `set`. `/sandbox` in the TUI shows the mode; the detailed view's header always does.

<!-- /memoria:section -->

<!-- memoria:section id="hooks" files="internal/hooks/hooks.go internal/hooks/exec.go internal/hooks/payload.go internal/hooks/trust.go internal/engine/embedded/pretooluse.go internal/engine/embedded/tools.go cmd/uah/hooks.go internal/app/setup.go internal/session/hooks.go" -->
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

<!-- /memoria:section -->

<!-- memoria:section id="configuration" files="internal/config/config.go cmd/uah/flags.go internal/app/resolve.go internal/app/setup.go .uagent/config.toml .uagent/hooks/guard.sh" -->
### Configuration

Two files, both TOML:

| File | Scope | Applies when |
| --- | --- | --- |
| `~/.config/uagent/config.toml` (or `$XDG_CONFIG_HOME/uagent/config.toml`, or `--config`) | Every workspace | Always |
| `<workspace>/.uagent/config.toml` | One workspace; overrides the user file, and its hooks add to the user file's | The user file lists the workspace under `[projects]` with `trusted = true`. Its hooks also need `uah hooks trust` |

Flags win over the environment, which wins over a resumed session's settings, then the project file, the user file, and the defaults. The names say `uagent` because uah shares uagent's directories. This repository's own [.uagent/config.toml](.uagent/config.toml) and [guard hook](.uagent/hooks/guard.sh) are a working example of a project file. A user file:

```toml
provider = "openai-codex"
model = "gpt-6-sol"
effort = "high"
timeout = "30m"
max_disk = "5G"
engine = "embedded"   # or "process"
fast = false          # priority processing
sandbox_mode = "workspace-write"   # read-only, workspace-write, danger-full-access

[instructions]
enabled = true
max_bytes = 32768

[sandbox_workspace_write]
network_access = false
writable_roots = ["~/Library/Caches/go-build"]   # ~ is home; relative paths are in the workspace

[shell_environment_policy]
inherit = "all"                   # all, core, none
ignore_default_excludes = true    # false drops *KEY*, *SECRET*, *TOKEN*
exclude = ["AWS_*"]

[tui]
details = false   # start in the detailed view

# A workspace's .uagent/config.toml applies only when trusted here.
[projects."/Users/me/code/proj"]
trusted = true
```

Unknown keys are errors, so a typo fails loudly instead of being ignored.

Design records and the documentation procedure are indexed in [docs](docs/README.md):

<!-- memoria:import src="docs/README.md#summary" -->
Design records for the harness, the TUI, state storage, and sandboxing, plus the architecture rules and documentation procedure for uagent-harness.
<!-- /memoria:import -->

The [TUI framework benchmark](bench/tui/README.md) holds the measurements behind choosing Bubble Tea v2 (a separate Go module).

<!-- /memoria:section -->

<!-- memoria:section id="development" files=".github/workflows/ci.yml .golangci.yml testing/fakellm/fakellm.go testing/harnesstest/harnesstest.go internal/engine/embedded/embedded_test.go" -->
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
<!-- /memoria:section -->
