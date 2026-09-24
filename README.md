<!-- memoria:section id="overview" files="cmd/uah/main.go go.mod" -->
# uah

`uah` is a terminal coding agent built on [uagent](https://github.com/viktordanov/uagent), the wrapper around unreal-agent-runner. It works like Codex: a TUI and a headless `uah run`, sessions you can resume, AGENTS.md and skills, a sandbox with approvals and auto-review, MCP servers, subagents, compaction, and hooks.

1. [Get started](#get-started)
2. [Common tasks](#common-tasks)
3. [Configuration](#configuration)
4. [How it works](#how-it-works)
5. [Development](#development)

The [ledger](docs/ledger.md) tracks what is built and what is next.
<!-- /memoria:section -->

<!-- memoria:section id="usage" files="cmd/uah/main.go cmd/uah/run.go cmd/uah/resume.go cmd/uah/sessions.go cmd/uah/tui.go cmd/uah/print.go cmd/uah/completion.go cmd/uah/doctor.go internal/app/doctor.go internal/app/doctorchecks.go cmd/uah/flags.go" -->
## Get started

1. Install it (Go 1.27.1 or later):

   ```sh
   go install github.com/viktordanov/uagent-harness/cmd/uah@latest
   ```

2. Sign in. The default provider, `openai-codex`, uses your ChatGPT login: run `codex login`. For another provider, pass `--provider` (openai, openrouter, fireworks, or ollama) and set its API key variable.
3. Check the setup: `uah doctor` checks the credentials, the sandbox, the configuration, hooks, MCP servers, and the state directory, and says how to fix each ✗.
4. Start in a repository:

   ```sh
   cd ~/code/proj
   uah                                       # the TUI
   uah "Fix the failing test in pkg/foo"     # the TUI, starting with a prompt
   ```

5. Add shell completion (bash, zsh, fish, or pwsh):

   ```sh
   uah completion zsh > "${fpath[1]}/_uah"
   ```

The keys to know in the TUI:

| Key | Does |
| --- | --- |
| enter | Send. While the agent works, the message queues and goes out when it finishes |
| ctrl+enter | Send now: the working agent reads it before its next model request |
| esc esc | Interrupt; queued messages stay |
| `/` | Commands, such as `/model`, `/effort`, `/compact`, `/context`, `/mcp`, `/agents`, `/status`, `/resume`, and `/new` |
| `@` | Mention a workspace file (fuzzy search) |
| ctrl+t | The detailed view: turns, tokens, and each tool's result |

The [TUI README](internal/tui/README.md) lists every key and command.

## Common tasks

### Resume work

```sh
uah resume              # pick one of this directory's sessions (--all: any directory)
uah resume --last       # this directory's most recent session
uah --session 3f2a      # a session by ID or unique prefix
```

In the TUI, ctrl+s opens the picker and ctrl+n starts a new session. The picker hides sessions from `uah run` and subagents, as Codex hides `codex exec` sessions.

### Run without the TUI

`uah run` prints progress on stderr and each answer on stdout, and exits when the agent is idle.

```sh
uah run "Fix the failing test in pkg/foo"
uah run --last "Now update the changelog"       # continue this directory's latest session
printf 'first\nsecond\n' | uah run --stdin       # each line is a message; lines queue while the agent works
uah run --stream "..."                           # JSONL events for scripts
```

It exits 0 when the run succeeds, 1 when it fails, 3 at the disk limit, 124 on a timeout, and 130 on an interrupt. Nobody can answer an approval headless, so commands that need one are declined with a reason. `uah run --help` lists the flags.

### Find an old session

```sh
uah sessions                           # this directory's sessions, newest first (--all: every directory)
uah sessions --search "flaky parser"   # sessions whose prompts or answers contain the words
uah sessions show 3f2a                 # the transcript (--json)
```

### Change the model or effort

- For this session: `/model gpt-6-luna` or `/effort low` in the TUI, or alt+, and alt+. to lower or raise the effort. On the embedded engine it applies from the next model request, even mid-run.
- At start: `uah -m gpt-6-luna -e medium`, and `--fast` for priority processing.
- For every session: `model` and `effort` in the [configuration](#configuration).

### Let a command run without asking

1. When uah asks, answer `s` ("Yes, and don't ask again"). It saves a rule for the command's prefix.
2. Or list prefixes in the configuration:

   ```toml
   [approvals]
   allow = ["go test", "git status"]
   forbid = ["git push --force"]
   ```

The [approvals README](internal/approval/README.md) gives the order in which rules, the sandbox, the auto-reviewer, hooks, and you decide.

### Add an MCP server

```sh
uah mcp add docs -- npx -y @example/docs-mcp            # a stdio server (--env KEY=VALUE)
uah mcp add linear --url https://mcp.linear.app/mcp     # an HTTP server
uah mcp login linear                                     # OAuth in the browser, if the server asks for it
uah mcp list                                             # every server, its status, and its auth
```

`uah mcp add` writes Codex's `[mcp_servers.<name>]` format into your user file, so a Codex configuration copies over. `/mcp` in the TUI shows each server and its tools; `/new` picks up a server added or logged in while the TUI runs.

### Give the agent instructions

Put them in `AGENTS.md` at the repository root or in any directory below it, as for Codex. To read `CLAUDE.md` too, set `project_doc_fallback_filenames = ["CLAUDE.md"]`. Skills go in `.agents/skills/<name>/SKILL.md`. `/context` shows how much of the context window they take.

### Delegate to subagents

Ask for it, for example "use two subagents to review the TUI and the store in parallel". The agent starts them with Codex's tools; each shows in the transcript as `AGENT <name>` with what it is doing, and `/agents` lists them. To define a kind of subagent, add a Codex role file:

```toml
# ~/.config/uagent/agents/reviewer.toml
name = "reviewer"
description = "Reviews a diff for bugs and missing tests."
model_reasoning_effort = "high"
developer_instructions = "Review only; do not edit files. List each finding with its file and line."
```

Each subagent runs on the parent's provider. To give one another model, effort, or fast mode:

- **Model and effort for one task.** Ask for it ("use a subagent on gpt-6-luna with low effort"). The agent passes `model` and `reasoning_effort` to `spawn_agent`. On openai-codex, a model outside Codex's catalog fails at once, with the models to choose from.
- **Model, effort, and fast mode for a kind of subagent.** Set `model`, `model_reasoning_effort`, and `service_tier = "priority"` in its role file; `service_tier = "priority"` is fast mode for that role only. See [role files](docs/configuration.md#subagents).
- **Defaults for every subagent.** Set `default_subagent_model` and `default_subagent_reasoning_effort` in `[agents]`. Fast mode for every subagent follows the parent: `/fast` in the session turns it on for the children it starts.

To hand a subagent the conversation so far, ask for a forked subagent ("fork a subagent to write the tests for what we just discussed"): `spawn_agent` with `fork_context` starts it from a copy of the parent's history, so it needs no exploring again and reuses the provider's prompt cache.

To watch a subagent work, type `/agents <name>` (tab completes the names): the TUI shows its transcript as it works, and a message you type there goes to it. esc returns to the main agent, which kept running. `uah sessions` lists subagents under their parent as `subagent-1a2b3c4d`, and `uah sessions show subagent-1a2b3c4d` prints one's transcript.

### Keep a long session going

uah compacts automatically at 90% of the context window. `/compact` compacts now, and `/context` shows what fills the window.

### Run a command at an event

Add a hook, for example a notification when the agent is idle:

```toml
[[hooks.Stop]]
command = "osascript -e 'display notification \"uah is idle\"'"
```

Hooks in a project's `.uagent/config.toml` run only after `uah hooks trust`; `uah hooks` lists them and whether each runs.

### See what is configured

`uah config` shows each setting's value and where it came from. `uah doctor` checks that everything works.
<!-- /memoria:section -->

<!-- memoria:section id="configuration" files="internal/config/config.go cmd/uah/flags.go cmd/uah/config.go internal/app/resolve.go internal/app/setup.go internal/app/explain.go internal/app/explain_files.go .uagent/config.toml .uagent/hooks/guard.sh" -->
## Configuration

Two TOML files:

| File | Applies to | When |
| --- | --- | --- |
| `~/.config/uagent/config.toml` (or `$XDG_CONFIG_HOME/uagent/config.toml`, or `--config`) | Every workspace | Always |
| `<workspace>/.uagent/config.toml` | One workspace | The user file marks the workspace `trusted` under `[projects]`; its hooks also need `uah hooks trust` |

A flag wins over the environment, which wins over a resumed session's settings, then the project file, the user file, and the defaults. Unknown keys are errors, so a typo fails loudly. The names say `uagent` because uah shares uagent's directories.

Every key, by group. The [configuration reference](docs/configuration.md) gives each one's type, default, flag, and merge rule, and the environment variables.

| Group | Keys |
| --- | --- |
| Model and engine | `provider`, `model`, `effort`, `fast`, `engine`, `timeout`, `max_disk` |
| Sandbox | `sandbox_mode`; `[sandbox_workspace_write]` `network_access`, `writable_roots`; `[shell_environment_policy]` `inherit`, `ignore_default_excludes`, `exclude`, `include_only`, `set` |
| Approvals | `approval_policy`, `approvals_reviewer`; `[approvals]` `allow`, `forbid`; `[review]` `model`, `effort`, `timeout` |
| Compaction | `auto_compact_percent`, `model_context_window` |
| Instructions and skills | `project_doc_fallback_filenames`, `project_root_markers`, `project_doc_max_bytes`; `[instructions]` `enabled`, `max_bytes` |
| Hooks | `[[hooks.<Event>]]` `matcher`, `command`, `timeout` |
| MCP servers | `[mcp_servers.<name>]` `command`, `args`, `env`, `env_vars`, `cwd`, `url`, `bearer_token_env_var`, `http_headers`, `env_http_headers`, `enabled`, `required`, `startup_timeout_sec`, `tool_timeout_sec`, `enabled_tools`, `disabled_tools`, `supports_parallel_tool_calls`, `default_tools_approval_mode`, `tools.<tool>.approval_mode`, `auth`, `scopes`, `oauth_resource`, `[oauth]`; `mcp_oauth_credentials_store`, `mcp_oauth_callback_port`, `mcp_oauth_callback_url` |
| Subagents | `[agents]` `enabled`, `max_concurrent_threads_per_session`, `max_depth`, `default_subagent_model`, `default_subagent_reasoning_effort` |
| TUI | `[tui]` `details` |
| Projects | `[projects."<path>"]` `trusted` |

A short user file:

```toml
model = "gpt-6-sol"
effort = "high"
sandbox_mode = "workspace-write"                 # read-only, workspace-write, danger-full-access
project_doc_fallback_filenames = ["CLAUDE.md"]   # also read CLAUDE.md

[approvals]
allow = ["go test", "git status"]

[mcp_servers.docs]
command = "npx"
args = ["-y", "@example/docs-mcp"]

[projects."/Users/me/code/proj"]
trusted = true   # apply this workspace's .uagent/config.toml
```

This repository's own [.uagent/config.toml](.uagent/config.toml) and [guard hook](.uagent/hooks/guard.sh) are a working project file.
<!-- /memoria:section -->

## How it works

Each part is documented next to its code. These are the summaries, with links to the full READMEs.

<!-- memoria:section id="tui" files="cmd/uah/tui.go" -->
### Sessions and the TUI

<!-- memoria:import src="internal/session/README.md#summary" -->
A session owns its settings, a message queue, at most one live run, pending approvals, and its hooks on one goroutine, and merges run events and its own events into one ordered stream. Messages queue while the agent works, a steer reaches the running agent when the engine allows it, and an interrupt keeps the queue.
<!-- /memoria:import -->

<!-- memoria:import src="internal/tui/README.md#summary" -->
The TUI is a pure reducer from session events and user intents to state and effects, a pure renderer from state to screen lines, and a thin Bubble Tea v2 shell that turns keys into intents and runs the effects against the session. Keys never change meaning: enter queues while the agent works, ctrl+enter sends now, and esc esc interrupts.
<!-- /memoria:import -->

Read more: [sessions](internal/session/README.md), [the session index](internal/store/README.md), and [the TUI](internal/tui/README.md) with its look. Diagnostics go to `<state-dir>/logs/uah-tui.log`.
<!-- /memoria:section -->

<!-- memoria:section id="engines" files="internal/app/resolve.go internal/app/setup.go" -->
### Engines

<!-- memoria:import src="internal/engine/README.md#summary" -->
An engine starts runs of unreal-agent-runner for a session: the embedded engine (the default) runs the runner's packages inside uah, so messages, model, effort, and fast mode reach a live run, and the process engine spawns the runner binary through uagent. Both keep uagent's guards, session lock, and run records, and write the same session files, so a session can move between them.
<!-- /memoria:import -->

`embedded` is the default; choose with `--engine` or `engine`. The [engine README](internal/engine/README.md) has a table of what each engine supports.
<!-- /memoria:section -->

<!-- memoria:section id="instructions" files="internal/app/setup.go internal/config/config.go" -->
### Instructions and skills

<!-- memoria:import src="internal/instructions/README.md#summary" -->
uah finds instruction files the way Codex does: the user's AGENTS.md, then one file per directory from the project root down to the workspace (AGENTS.override.md, else AGENTS.md, else a configured fallback such as CLAUDE.md). They are joined, capped at 32 KiB, and placed after the runner's default host prompt; skills come from Codex's skill folders.
<!-- /memoria:import -->

`--no-instructions` turns this off. Read more: [instructions](internal/instructions/README.md).
<!-- /memoria:section -->

<!-- memoria:section id="sandbox" files="internal/app/setup.go internal/app/resolve.go" -->
### Sandbox

<!-- memoria:import src="internal/sandbox/README.md#summary" -->
Commands run in the operating system's sandbox, as in Codex: Seatbelt on macOS and bubblewrap on Linux. The default mode, workspace-write, lets commands read the whole disk and write only the workspace and temporary directories, without network, and keeps .git, .uagent, .agents, and .codex read-only.
<!-- /memoria:import -->

Read more: [sandbox](internal/sandbox/README.md).
<!-- /memoria:section -->

<!-- memoria:section id="approvals" files="internal/app/approvals.go internal/app/review.go" -->
### Approvals, rules, and auto-review

<!-- memoria:import src="internal/approval/README.md#summary" -->
On the embedded engine, each command runs in the sandbox unless a rule or an approval says otherwise: a command rule can allow, forbid, or ask; the model can ask to run a command outside the sandbox; and an escalation goes to the auto-reviewer, then PermissionRequest hooks, then the user. The defaults are Codex's: workspace-write, on-request, and auto-review.
<!-- /memoria:import -->

Read more: [approvals](internal/approval/README.md), [rules](internal/rules/README.md), and [auto-review](internal/review/README.md).
<!-- /memoria:section -->

<!-- memoria:section id="hooks" files="cmd/uah/hooks.go internal/app/setup.go" -->
### Hooks

<!-- memoria:import src="internal/hooks/README.md#summary" -->
Hooks run a command at a session event with Claude Code's contract: the event arrives as JSON on stdin, exit 0 continues, exit 2 blocks with stderr as the reason, and any other exit is reported and ignored. Project hooks run only after `uah hooks trust` records their exact commands and the content of any local script they run.
<!-- /memoria:import -->

Read more: [hooks](internal/hooks/README.md), with every event and its payload.
<!-- /memoria:section -->

<!-- memoria:section id="mcp" files="internal/app/mcp.go internal/app/mcpcli.go cmd/uah/mcp.go cmd/uah/mcpprint.go" -->
### MCP servers

<!-- memoria:import src="internal/mcp/README.md#summary" -->
uah runs the MCP servers in `[mcp_servers]` (Codex's format) on the embedded engine through the official Go SDK: stdio and streamable HTTP servers, their tools offered as `mcp__<server>__<tool>` and called without blocking the agent, Codex's approval modes, OAuth logins with `uah mcp login` kept in the OS keyring, and `uah mcp` to list, add, and remove servers.
<!-- /memoria:import -->

- Servers start on the first run (or `/mcp`) and stop with the session. One that fails to start is left out, unless it has `required = true`.
- A call runs in the background, so the agent keeps working. A tool asks for approval by its `approval_mode`, as in Codex.
- OAuth tokens are kept in the OS keyring (or a 0600 file without one) and refreshed as they expire. A server that needs a login shows "needs login" in `/mcp` and `uah doctor`.

Read more: [MCP](internal/mcp/README.md), and the [design and validation](docs/design/mcp.md).
<!-- /memoria:section -->

<!-- memoria:section id="subagents" files="internal/app/agents.go" -->
### Subagents

<!-- memoria:import src="internal/agents/README.md#summary" -->
Subagents are child sessions that a session's agent starts, messages, waits for, and closes through Codex's v1 multi-agent tools. `internal/agents` implements them behind the `engine.Subagents` seam: the embedded engine offers the tools and runs their calls in the background, and the package owns the tools, the children's lifecycle, approvals through the parent, limits, hooks, and resume.
<!-- /memoria:import -->

A subagent is the same as the main agent in every way except its session, which is nested under the parent's: the same instructions, skills, sandbox, approvals, hooks, MCP servers, and compaction. It asks for approval through the parent's session.

- Its session ID is `subagent-<uuid>`. A role or the spawn call can give it another model, effort, or fast mode on the parent's provider.
- `fork_context` starts it from a copy of the parent's history, so its first model request starts with the parent's and reuses the provider's prompt cache.
- A failed subagent reports why, such as the provider's message, to the parent's `wait_agent`, the TUI, and `uah run`.
- `/agents <name>` shows its live transcript in the TUI.

Read more: [subagents](internal/agents/README.md), and the [design and validation](docs/design/subagents.md).
<!-- /memoria:section -->

<!-- memoria:section id="compaction" files="internal/llmcall/llmcall.go cmd/uah/stream.go internal/contextusage/usage.go" -->
### Compaction and `/context`

<!-- memoria:import src="internal/compaction/README.md#summary" -->
uah compacts a long conversation as Codex does: the earlier user messages stay verbatim and in order, up to the newest 20,000 tokens of them, and the rest is replaced by a model-written handoff summary. The session file keeps the full history; only what goes to the model changes, and a compaction is saved next to the session so a resumed session keeps it.
<!-- /memoria:import -->

`/context` shows what fills the window, as Claude Code's does: a 10×10 grid, one cell per percent, with each category's tokens (system prompt, instruction files, skills, tools, MCP tools, your messages, agent messages, and tool calls with their results), the free space, and the auto-compact buffer, then a line per file, skill, and tool. It breaks down the last request sent, estimated at 4 bytes a token and scaled to the input tokens the provider reported (`internal/contextusage`).

Read more: [compaction](internal/compaction/README.md), and the [design and validation](docs/design/compaction.md).
<!-- /memoria:section -->

<!-- memoria:section id="development" files=".github/workflows/ci.yml .golangci.yml testing/fakellm/fakellm.go testing/harnesstest/harnesstest.go" -->
## Development

Tests need no model or tokens: the process engine runs against uagent's fake runner, and the embedded engine against `testing/fakellm`, a scripted Responses API. One test drives the real `unreal-agent-runner` and the embedded engine with the same script and requires the same events; `go test -short` skips it.

```sh
go run ./cmd/uah --version   # build and run
go test -race ./...          # unit and end-to-end tests
golangci-lint run ./...      # lint (golangci-lint v2.13.2)
```

CI runs the build, the race tests, and the linter on each push. Design records, the architecture rules, and the documentation procedure are in [docs](docs/README.md):

<!-- memoria:import src="docs/README.md#summary" -->
The configuration reference, design records for the harness, the TUI, state storage, sandboxing, compaction, MCP, and subagents, plus the architecture rules and documentation procedure for uagent-harness.
<!-- /memoria:import -->

`bench/tui` is a separate Go module with the benchmark behind choosing Bubble Tea v2.
<!-- /memoria:section -->
