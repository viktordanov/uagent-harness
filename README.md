<!-- memoria:section id="overview" files="cmd/uah/main.go go.mod" -->
# uagent-harness

The general-purpose harness built on [uagent](https://github.com/viktordanov/uagent), the wrapper around unreal-agent-runner.
uagent runs one task with safety guards. This repository adds what long-lived, interactive work needs: sessions with a message queue and steering, an embedded engine for live model, effort, and fast-mode changes, instruction files, configuration, hooks, and a terminal UI.

Status: the [ledger](docs/ledger.md) tracks the work: sessions, the TUI, both engines, instructions and skills, hooks, the sandbox with approvals and auto-review, compaction, MCP, and the session index are built.

1. [Use it](#use-it): the TUI, engines, instructions, hooks, and configuration
2. [Development](#development)
<!-- /memoria:section -->

<!-- memoria:section id="usage" files="cmd/uah/main.go cmd/uah/run.go cmd/uah/resume.go cmd/uah/sessions.go cmd/uah/tui.go cmd/uah/print.go cmd/uah/completion.go cmd/uah/doctor.go internal/app/doctor.go internal/app/doctorchecks.go cmd/uah/flags.go" -->
## Use it

```sh
go install github.com/viktordanov/uagent-harness/cmd/uah@latest

uah                                                        # the TUI: a live session in the current directory
uah -C ~/code/proj "Fix the failing test in pkg/foo"       # the TUI, starting with a prompt
uah resume                                                 # pick a session of this directory to resume (--all: any directory); uah run sessions are hidden, as Codex hides exec sessions
uah resume --last                                          # resume this directory's most recent session
uah --session 3f2a                                         # the TUI, resuming a session with its transcript
uah --fast                                                 # priority processing (openai and openai-codex)
uah doctor                                                 # check the runner, credentials, sandbox, config, hooks, MCP servers, and state (--json; exit 1 on a ✗)

uah run -C ~/code/proj "Fix the failing test in pkg/foo"   # a session: progress on stderr, answers on stdout
uah sessions                                               # this directory's sessions, most recent first (--all: every directory)
uah sessions --search "flaky parser"                       # sessions whose prompts or answers contain the words
uah sessions show 3f2a                                     # a transcript, by ID or unique prefix
uah completion zsh > "${fpath[1]}/_uah"                    # shell completion: bash, zsh, fish, pwsh
uah run --session 3f2a "Now update the README"             # resume with the session's model, effort, and workspace
uah run --last -m gpt-6-luna "And the changelog"           # resume this directory's latest session with another model
printf 'first\nsecond\n' | uah run --stdin                  # each line is a message; lines queue while the agent works
uah run --stream "..."                                     # JSONL: uagent's run events plus session events
```

`uah run` takes the same backend, guard, and state flags as uagent (`--provider`, `-m`, `-e`, `-t`, `-C`, `--state-dir`, `--runner`, `--max-disk`, `--allow-dotenv`); `uah run --help` lists them.
A flag wins over the environment (`UNREAL_HARNESS_LLM_*`, `UAGENT_*`), which wins over the resumed session's settings and the defaults. Sessions and run records live in uagent's state directory, so `uagent` and `uah` share them.

Both the TUI and `uah run` drive a [session](internal/session/README.md):

<!-- memoria:import src="internal/session/README.md#summary" -->
A session owns its settings, a message queue, at most one live run, pending approvals, and its hooks on one goroutine, and merges run events and its own events into one ordered stream. Messages queue while the agent works, a steer reaches the running agent when the engine allows it, and an interrupt keeps the queue.
<!-- /memoria:import -->

<!-- /memoria:section -->

<!-- memoria:section id="tui" files="cmd/uah/tui.go" -->
### The TUI

<!-- memoria:import src="internal/tui/README.md#summary" -->
The TUI is a pure reducer from session events and user intents to state and effects, a pure renderer from state to screen lines, and a thin Bubble Tea v2 shell that turns keys into intents and runs the effects against the session. Keys never change meaning: enter queues while the agent works, ctrl+enter sends now, and esc esc interrupts.
<!-- /memoria:import -->

The [TUI README](internal/tui/README.md) lists every key and slash command, and explains how the reducer, the renderer, and the Bubble Tea shell fit together. Diagnostics go to `<state-dir>/logs/uah-tui.log`, and `[tui] details = true` starts in the detailed view.

<!-- /memoria:section -->

<!-- memoria:section id="engines" files="internal/app/resolve.go internal/app/setup.go" -->
### Engines

<!-- memoria:import src="internal/engine/README.md#summary" -->
An engine starts runs of unreal-agent-runner for a session: the embedded engine (the default) runs the runner's packages inside uah, so messages, model, effort, and fast mode reach a live run, and the process engine spawns the runner binary through uagent. Both keep uagent's guards, session lock, and run records, and write the same session files, so a session can move between them.
<!-- /memoria:import -->

Choose one with `--engine`, `UAH_ENGINE`, or `engine` in the configuration; `embedded` is the default. The [engine README](internal/engine/README.md) has a table of what each engine supports, and explains the embedded engine's wiring and remote jobs.

<!-- /memoria:section -->

<!-- memoria:section id="compaction" files="internal/compaction/compaction.go internal/compaction/window.go internal/engine/embedded/compact.go internal/engine/embedded/compactlog.go internal/llmcall/llmcall.go internal/session/compact.go internal/tui/state/context.go internal/contextusage/usage.go internal/engine/embedded/context.go internal/tui/state/contextview.go internal/tui/render/contextview.go" -->
### Compaction

On the embedded engine, uah compacts a long conversation the way Codex does. It asks the model for a handoff summary, then sends every earlier user message verbatim and in order, followed by the summary. The model's replies, reasoning, tool calls, and tool outputs before that point are dropped. Messages sent after the compaction follow the summary.

- `/compact` compacts before the next model request: now if the agent is working, or with the next message if it is idle.
- Automatic compaction starts before a model request when the last response used `auto_compact_percent` of the model's context window (default 90, as Codex; 0 turns it off).
- The window comes from Codex's model table (272,000 tokens for current models and for models it does not know). `model_context_window` overrides it.

The footer shows "N% context left", computed from the last response's tokens as Codex computes it. A compaction is saved in `sessions/<id>.compaction.jsonl` next to the session file, so a resumed session keeps it. The runner's session file keeps the full history. The process engine cannot compact. The [plan](docs/design/compaction.md) lists the choices made.

`/context` shows what fills the window, as Claude Code's `/context` does: a 10×10 grid, one cell per percent, colored by category, beside a legend with each category's tokens: system prompt, instruction files, skills, tools, MCP tools, your messages, agent messages, and tool calls with their results, then the free space and the auto-compact buffer. Below it, each instruction file, skill, and tool has its own line. It breaks down the last request the engine sent, after any compaction: each part is estimated at 4 bytes a token, as Codex estimates, and scaled so the parts add up to the input tokens the provider reported (`internal/contextusage`). It needs the embedded engine and one model request.

<!-- /memoria:section -->

<!-- memoria:section id="instructions" files="internal/app/setup.go internal/config/config.go" -->
### Instructions and skills

<!-- memoria:import src="internal/instructions/README.md#summary" -->
uah finds instruction files the way Codex does: the user's AGENTS.md, then one file per directory from the project root down to the workspace (AGENTS.override.md, else AGENTS.md, else a configured fallback such as CLAUDE.md). They are joined, capped at 32 KiB, and placed after the runner's default host prompt; skills come from Codex's skill folders.
<!-- /memoria:import -->

`--no-instructions` turns this off. The [instructions README](internal/instructions/README.md) gives the discovery order, the size cap, and the skill folders.

<!-- /memoria:section -->

<!-- memoria:section id="sandbox" files="internal/app/setup.go internal/app/resolve.go" -->
### Sandbox

<!-- memoria:import src="internal/sandbox/README.md#summary" -->
Commands run in the operating system's sandbox, as in Codex: Seatbelt on macOS and bubblewrap on Linux. The default mode, workspace-write, lets commands read the whole disk and write only the workspace and temporary directories, without network, and keeps .git, .uagent, .agents, and .codex read-only.
<!-- /memoria:import -->

Set the mode with `--sandbox`, `UAH_SANDBOX`, or `sandbox_mode`, and see it with `/sandbox` in the TUI. The [sandbox README](internal/sandbox/README.md) covers the modes, the protected paths, both platforms, and the environment policy.

<!-- /memoria:section -->

<!-- memoria:section id="approvals" files="internal/app/approvals.go internal/app/review.go" -->
### Approvals and rules

<!-- memoria:import src="internal/approval/README.md#summary" -->
On the embedded engine, each command runs in the sandbox unless a rule or an approval says otherwise: a command rule can allow, forbid, or ask; the model can ask to run a command outside the sandbox; and an escalation goes to the auto-reviewer, then PermissionRequest hooks, then the user. The defaults are Codex's: workspace-write, on-request, and auto-review.
<!-- /memoria:import -->

The [approvals README](internal/approval/README.md) walks through the whole pipeline and its defaults. The [rules README](internal/rules/README.md) gives the `.rules` format, and the [auto-review README](internal/review/README.md) explains the reviewer.

<!-- /memoria:section -->

<!-- memoria:section id="hooks" files="cmd/uah/hooks.go internal/app/setup.go" -->
### Hooks

<!-- memoria:import src="internal/hooks/README.md#summary" -->
Hooks run a command at a session event with Claude Code's contract: the event arrives as JSON on stdin, exit 0 continues, exit 2 blocks with stderr as the reason, and any other exit is reported and ignored. Project hooks run only after `uah hooks trust` records their exact commands and the content of any local script they run.
<!-- /memoria:import -->

`uah hooks` lists the hooks for a workspace and whether each runs, and `uah hooks trust` trusts a project's hooks. The [hooks README](internal/hooks/README.md) has every event, the payload, and the trust rules.

<!-- /memoria:section -->

<!-- memoria:section id="mcp" files="internal/mcp/config.go internal/mcp/manager.go internal/mcp/names.go internal/mcp/result.go internal/mcp/transport.go internal/engine/embedded/mcptool.go internal/engine/embedded/mcpjobs.go internal/app/mcp.go internal/tui/state/mcp.go" -->
### MCP servers

On the embedded engine, uah starts the MCP servers in `[mcp_servers]` and offers their tools to the model as `mcp__<server>__<tool>`. The configuration is Codex's, so a Codex `[mcp_servers]` section copies over:

```toml
[mcp_servers.docs]               # stdio
command = "npx"
args = ["-y", "@example/docs-mcp"]
env = { DOCS_LANG = "en" }       # env_vars = ["NAME"] passes a variable through
startup_timeout_sec = 20         # default 30
tool_timeout_sec = 60            # default 300
disabled_tools = ["delete_page"] # or enabled_tools = [...] to allow only those

[mcp_servers.docs.tools.search]
approval_mode = "approve"        # auto (default), prompt, writes, approve

[mcp_servers.tracker]            # streamable HTTP
url = "https://mcp.example.com/mcp"
bearer_token_env_var = "TRACKER_TOKEN"
```

Servers start on the first run (or `/mcp`) and stop when the session closes; a stdio server gets only `HOME`, `PATH`, `USER`, and a few other basic variables unless `env` or `env_vars` adds more, as in Codex. A server that fails to start is left out (`required = true` fails the run instead). A call runs in the background, so the model keeps working while it runs; a call past its timeout, or to a server that crashed, returns an error to the model. Results reach the model as text and images; structured content arrives as JSON text. Disabled tools are hidden. A tool whose `approval_mode` needs approval asks through the same prompt as sandbox escalations, as in Codex: `prompt` always, `writes` unless the tool is read-only, and `auto` (the default) unless its annotations say it is read-only, or both non-destructive and closed-world. Headless runs and `approval_policy = "never"` refuse such calls with a reason. PreToolUse hook matchers see the `mcp__` names. `/mcp` lists each server, its state, and its tools. The process engine does not run MCP servers and says so. Unsupported Codex keys (OAuth, `bearer_token`, `http_headers_helper`) are errors ([plan](docs/design/mcp.md)).

<!-- /memoria:section -->

<!-- memoria:section id="subagents" files="internal/agents/manager.go internal/agents/child.go internal/agents/roles.go internal/engine/subagents.go internal/engine/embedded/agenttool.go internal/engine/embedded/agentjobs.go internal/engine/embedded/agentprompt.go internal/app/agents.go internal/tui/state/agents.go" -->
### Subagents

On the embedded engine, the agent can start subagents with Codex's v1 tools. The tool description tells the model, as Codex's does, to spawn only when you or AGENTS.md ask for delegation or parallel work.

| Tool | Does |
| --- | --- |
| `spawn_agent(message, agent_type?, model?, reasoning_effort?)` | Starts a subagent with the task and returns `{id, nickname}` at once |
| `send_input(id, message)` | Gives a running or finished subagent another message |
| `wait(ids, timeout_ms?)` | Returns when any listed subagent finishes, with each finished one's final answer, or `timed_out` after the timeout (default 30s, 10s to 1h) |
| `close_agent(id)` | Stops a subagent and returns its status before it stopped |

The tools run in the background, as MCP calls do, so the agent keeps working while a subagent runs or while it waits. A subagent is an ordinary session in the same workspace, with the parent's provider, model, effort, instructions, sandbox, and MCP servers. It asks for approval through the parent's session: the prompt's reason starts with `agent <nickname>:`. With no one to ask (`uah run`), such commands are declined with a reason. The TUI shows each subagent as one line where it was spawned (`• agent Ada: running 0:42`, then `done`), and `/agents` lists them with their IDs. `uah sessions` lists subagents under their parent, and the resume picker hides them; `uah resume <id>` opens one like any session.

```toml
[agents]
enabled = true                              # default
max_concurrent_threads_per_session = 4      # open subagents per session tree (Codex's max_threads also works)
max_depth = 1                               # 1: subagents cannot spawn their own
default_subagent_model = "gpt-6-luna"       # default: the parent's model
default_subagent_reasoning_effort = "medium" # default: the parent's effort
```

Agent types are Codex role files: `~/.config/uagent/agents/*.toml`, and `<workspace>/.uagent/agents/*.toml` in a trusted workspace, which replaces a user role of the same name. The `spawn_agent` description lists them.

```toml
name = "reviewer"
description = "Reviews a diff for bugs and missing tests."
nickname_candidates = ["Rex", "Rita"]   # optional
model = "gpt-6-sol"                     # optional
model_reasoning_effort = "high"         # optional
developer_instructions = "Review only; do not edit files. List each finding with its file and line."
```

uah reads these keys of a role file; other Codex config keys in it are ignored with a warning, and a file without a name, description, or `developer_instructions` is skipped with a warning. The process engine does not run subagents ([plan and as-built notes](docs/design/subagents.md)).

<!-- /memoria:section -->

<!-- memoria:section id="configuration" files="internal/config/config.go cmd/uah/flags.go cmd/uah/config.go internal/app/resolve.go internal/app/setup.go internal/app/explain.go internal/app/explain_files.go .uagent/config.toml .uagent/hooks/guard.sh" -->
### Configuration

Two files, both TOML:

| File | Scope | Applies when |
| --- | --- | --- |
| `~/.config/uagent/config.toml` (or `$XDG_CONFIG_HOME/uagent/config.toml`, or `--config`) | Every workspace | Always |
| `<workspace>/.uagent/config.toml` | One workspace; overrides the user file, and its hooks, `writable_roots`, and `[approvals]` lists add to the user file's | The user file lists the workspace under `[projects]` with `trusted = true`. Its hooks also need `uah hooks trust` |

Flags win over the environment, which wins over a resumed session's settings, then the project file, the user file, and the defaults. The names say `uagent` because uah shares uagent's directories. The [configuration reference](docs/configuration.md) lists every key with its type, default, flag, and how a project file merges with the user file, the exceptions to this order, and a complete example of each file. `uah config` shows the effective value of each key for a workspace and where it came from (`--json` for scripts). This repository's own [.uagent/config.toml](.uagent/config.toml) and [guard hook](.uagent/hooks/guard.sh) are a working example of a project file. A short user file:

```toml
model = "gpt-6-sol"
effort = "high"
sandbox_mode = "workspace-write"                 # read-only, workspace-write, danger-full-access
project_doc_fallback_filenames = ["CLAUDE.md"]   # Codex's key: also read CLAUDE.md

[approvals]
allow = ["go test", "git status"]   # command prefixes that run outside the sandbox without asking

[tui]
details = true   # start in the detailed view

[agents]
max_concurrent_threads_per_session = 4   # see Subagents for every key

# A workspace's .uagent/config.toml applies only when trusted here.
[projects."/Users/me/code/proj"]
trusted = true
```

Unknown keys are errors, so a typo fails loudly instead of being ignored.

Design records and the documentation procedure are indexed in [docs](docs/README.md):

<!-- memoria:import src="docs/README.md#summary" -->
The configuration reference, design records for the harness, the TUI, state storage, sandboxing, compaction, MCP, and subagents, plus the architecture rules and documentation procedure for uagent-harness.
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
