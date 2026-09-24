<!-- memoria:section id="overview" files="cmd/uah/main.go go.mod" -->
# uagent-harness

The general-purpose harness built on [uagent](https://github.com/viktordanov/uagent), the wrapper around unreal-agent-runner.
uagent runs one task with safety guards. This repository adds what long-lived, interactive work needs: sessions with a message queue and steering, an embedded engine for live model, effort, and fast-mode changes, instruction files, configuration, hooks, and a terminal UI.

Status: the [ledger](docs/ledger.md) tracks the work: sessions, the TUI, both engines, instructions and skills, hooks, the sandbox with approvals and auto-review, compaction, MCP, and the session index are built.

1. [Use it](#use-it): the TUI, engines, instructions, hooks, and configuration
2. [Development](#development)
<!-- /memoria:section -->

<!-- memoria:section id="usage" files="cmd/uah/main.go cmd/uah/run.go cmd/uah/resume.go cmd/uah/sessions.go cmd/uah/tui.go cmd/uah/print.go cmd/uah/doctor.go internal/app/doctor.go internal/app/doctorchecks.go internal/session/history.go internal/session/sidecar.go internal/store/store.go internal/store/query.go" -->
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
uah run --session 3f2a "Now update the README"             # resume with the session's model, effort, and workspace
uah run --last -m gpt-6-luna "And the changelog"           # resume this directory's latest session with another model
printf 'first\nsecond\n' | uah run --stdin                  # each line is a message; lines queue while the agent works
uah run --stream "..."                                     # JSONL: uagent's run events plus session events
```

<!-- /memoria:section -->

<!-- memoria:section id="tui" files="internal/tui/state/commands.go internal/tui/state/reduce.go internal/tui/bubble/keys.go internal/tui/bubble/model.go internal/tui/render/items.go internal/tui/render/screen.go internal/tui/render/markdown.go internal/tui/state/menu.go internal/tui/bubble/files.go" -->
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
| `/` or `@` then tab, ↑/↓, enter, esc | The menu: commands and their values after `/`, workspace files (fuzzy) after `@`. Tab fills the selection in, enter runs a command, esc closes the menu |
| ctrl+t | Compact or detailed view |
| ctrl+r | Show or hide reasoning summaries |
| mouse wheel, shift+↑ / shift+↓, pgup / pgdn | Scroll the transcript; end returns to the bottom. While the TUI reports the mouse, select text with Option (iTerm2, Terminal) or Shift (most others) held |
| ctrl+c | Clear the composer; on an empty composer, quit (twice while a run is live) |

Commands: `/model <id>`, `/effort <level>`, `/compact`, `/resume [id]`, `/new`, `/stop`, `/status` (with a 12-week activity heatmap), `/mcp`, `/agents`, `/details`, `/reasoning`, `/help`, `/quit`. `/model`, `/effort`, and `/fast` apply from the next model request on the embedded engine, and from the next run on the process engine. `/fast` needs the embedded engine and the openai or openai-codex provider.
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

<!-- memoria:section id="compaction" files="internal/compaction/compaction.go internal/compaction/window.go internal/engine/embedded/compact.go internal/engine/embedded/compactlog.go internal/llmcall/llmcall.go internal/session/compact.go internal/tui/state/context.go" -->
### Compaction

On the embedded engine, uah compacts a long conversation the way Codex does. It asks the model for a handoff summary, then sends every earlier user message verbatim and in order, followed by the summary. The model's replies, reasoning, tool calls, and tool outputs before that point are dropped. Messages sent after the compaction follow the summary.

- `/compact` compacts before the next model request: now if the agent is working, or with the next message if it is idle.
- Automatic compaction starts before a model request when the last response used `auto_compact_percent` of the model's context window (default 90, as Codex; 0 turns it off).
- The window comes from Codex's model table (272,000 tokens for current models and for models it does not know). `model_context_window` overrides it.

The footer shows "N% context left", computed from the last response's tokens as Codex computes it. A compaction is saved in `sessions/<id>.compaction.jsonl` next to the session file, so a resumed session keeps it. The runner's session file keeps the full history. The process engine cannot compact. The [plan](docs/design/compaction.md) lists the choices made.

<!-- /memoria:section -->

<!-- memoria:section id="instructions" files="internal/instructions/instructions.go internal/app/setup.go internal/engine/embedded/skills.go internal/config/config.go" -->
### Instructions and skills

The runner reads no instruction files, so `uah` builds them into the runner's system prompt, after the runner's own default text, the way Codex finds them (codex-rs/core/src/agents_md.rs):

1. The user file: `~/.config/uagent/AGENTS.md`, or else `~/.codex/AGENTS.md`.
2. One file per directory from the project root down to the workspace: `AGENTS.override.md`, else `AGENTS.md`, else the first of `project_doc_fallback_filenames` (none by default; `["CLAUDE.md"]` reads Claude Code's files). The project root is the nearest ancestor with one of `project_root_markers` (`.git` by default; `[]` means the workspace only).

Later files are more specific. Blank files are skipped, and the total stops at `project_doc_max_bytes` (32 KiB). `--no-instructions` turns this off, and the loaded files are reported as `instructions_loaded`.

Skills are Codex's `<name>/SKILL.md` folders, offered through the runner's own skill tool. They are read from `.agents/skills` in each directory from the workspace up to the project root, the runner's `.harness/skills`, `~/.config/uagent/skills`, and `$CODEX_HOME/skills`; a name in a more specific place wins.

<!-- /memoria:section -->

<!-- memoria:section id="sandbox" files="internal/sandbox/sandbox.go internal/sandbox/shell.go internal/sandbox/seatbelt.go internal/sandbox/bwrap.go internal/sandbox/denied.go internal/sandbox/env.go internal/engine/embedded/sandboxtool.go internal/engine/embedded/tools.go internal/app/setup.go internal/app/resolve.go" -->
### Sandbox

Commands run in the operating system's sandbox, as in Codex: Seatbelt (`sandbox-exec`) on macOS and bubblewrap (`bwrap`, which must be installed) on Linux. The mode comes from `--sandbox`, `UAH_SANDBOX`, or `sandbox_mode`:

| Mode | Commands can |
| --- | --- |
| `workspace-write` (default) | Read any file; write the workspace, `/tmp`, `$TMPDIR`, and `writable_roots`, except `.git`, `.uagent`, `.agents`, and `.codex`; no network unless `network_access = true` |
| `read-only` | Read any file; write nothing; no network |
| `danger-full-access` | Anything your user can: no sandbox |

When a command fails in a way that looks like the sandbox blocked it, the model is told so and can ask to run it outside the sandbox ([approvals](#approvals-and-rules)). On the process engine, the runner's `SHELL` is a script that sandboxes each command; it cannot ask. Where no sandbox is available, uah says so; on the embedded engine each command then asks for approval unless a rule allows it, and on the process engine commands run without one. On Linux, a protected name that does not exist yet (such as `.git` in a workspace that is not a repository root) is not protected, because bubblewrap can only cover existing paths; macOS protects it either way.

Commands get the whole environment, as in Codex. `[shell_environment_policy]` narrows it with Codex's keys: `inherit` (`all`, `core`, `none`), `ignore_default_excludes = false` to drop names matching `*KEY*`, `*SECRET*`, `*TOKEN*`, `exclude` and `include_only` patterns, and `set`. `/sandbox` in the TUI shows the mode; the detailed view's header always does.

<!-- /memoria:section -->

<!-- memoria:section id="approvals" files="internal/approval/approval.go internal/approval/prefix.go internal/rules/rules.go internal/rules/parse.go internal/rules/shell.go internal/rules/load.go internal/engine/embedded/sandboxtool.go internal/session/approvals.go internal/tui/state/approval.go internal/tui/render/approval.go internal/app/approvals.go internal/review/review.go internal/engine/embedded/autoreview.go internal/app/review.go" -->
### Approvals and rules

On the embedded engine, the model can ask to run a command outside the sandbox (`sandbox_permissions: "require_escalated"` with a `justification`), as in Codex (checked against rust-v0.156.1). The approval policy (`--ask`, `UAH_ASK`, or `approval_policy`) decides who answers:

| Policy | An escalation, or a command a `prompt` rule matches |
| --- | --- |
| `on-request` (default) | The TUI asks: "Yes, proceed" (`y`), "Yes, and don't ask again for commands that start with `<prefix>`" (`s`, when uah can propose a prefix), or "No, and tell the agent what to do differently" (`n` or esc). `uah run` has no one to ask and denies |
| `never` | Denied |

A denied command is not run, and the model gets the reason. The agent waits while the prompt is open; an interrupt declines it. An approved escalation runs outside the sandbox, with the network.

Before anyone is asked, the auto-reviewer judges the action, as Codex's `approvals_reviewer = "auto_review"` does (the default; `"user"` turns it off). It is one model call with the user's messages as trusted context, the latest tool calls without their output as untrusted context, and Codex's review policy: `codex-auto-review` at low effort on openai-codex (a real probe used about 3,500 input tokens and 6 seconds), the session's model at low effort elsewhere; `[review] model`, `effort`, and `timeout` override it. It allows or denies with a reason shown in the TUI ("auto-approved (low risk): …"); a failed review denies. After three denials in a row it steps aside and the user (or a PermissionRequest hook) decides until the next user message. The same applies to MCP calls that need approval, and in `uah run`, where the reviewer can approve without a user.

Command rules are Codex's `.rules` files, Starlark `prefix_rule` calls, read from `~/.config/uagent/rules/*.rules` and, for a trusted workspace, `<workspace>/.uagent/rules/*.rules`:

```python
prefix_rule(
    pattern = ["git", ["push", "fetch"]],         # words; a list is alternatives
    decision = "prompt",                           # allow (default), prompt, forbidden
    justification = "Pushing changes the remote",  # shown when asking or forbidding
    match = ["git push origin"],                   # examples, checked when the file loads
    not_match = ["git status"],
)
```

A rule matches when its pattern is a prefix of the command's words. A command such as `a && b | c` is split into its simple commands; the strictest decision wins (`forbidden`, then `prompt`, then `allow`), and `allow` needs every part allowed. A command with redirects, variables, substitutions, or control flow matches no rule. `allow` runs the command outside the sandbox without asking; `forbidden` never runs it and tells the model the justification. "Don't ask again" appends `prefix_rule(pattern=[...], decision="allow")` to `~/.config/uagent/rules/default.rules` and applies it at once. It proposes the model's `prefix_rule` suggestion, or else the whole command, but never a bare shell, interpreter, `git`, `rm`, `sudo`, or `env` (Codex's list).

For simple cases, `[approvals]` in `config.toml` lists command prefixes: `allow` runs them outside the sandbox without asking, and `forbid` never runs them. A trusted project file adds to the user file's lists, and its `writable_roots` add to the user's (relative roots are in the workspace).

<!-- /memoria:section -->

<!-- memoria:section id="hooks" files="internal/hooks/hooks.go internal/hooks/exec.go internal/hooks/payload.go internal/hooks/trust.go internal/hooks/script.go internal/engine/embedded/pretooluse.go internal/engine/embedded/tools.go cmd/uah/hooks.go internal/app/setup.go internal/session/hooks.go" -->
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
| `PermissionRequest` | Before the user is asked to approve an escalated command, a `prompt` rule, or an MCP call (`tool_name` is `Bash` or the `mcp__` name) | Answer for the user with `permissionDecision` `"allow"` or `"deny"` (exit 2 denies); works headless too |
| `PreCompact` | A compaction is about to start (`trigger`: manual or auto), on the embedded engine | Stop it (exit 2 or `"decision":"block"`) |
| `SessionEnd` | The session closes | Observe only, with at most a second |

Hooks in the user file run as written. Hooks in a trusted project's `.uagent/config.toml` run only after `uah hooks trust` records their exact commands (by SHA-256, in `~/.config/uagent/trusted-hooks.json`); a changed command needs trust again. When a command runs a local script (its first word is a path to a file, absolute or relative to the workspace, such as `.uagent/hooks/check.sh` or `"$UAH_PROJECT_DIR"/check.sh`), trust also records the script's SHA-256, so an edited script is reported as untrusted ("the script changed") until `uah hooks trust` runs again. Entries trusted before uah hashed scripts still cover commands that run no script; commands that run one need trust again. `uah hooks` lists the hooks for a workspace and whether each runs. Hook runs appear in the TUI's detailed view (ctrl+t); blocks and failures appear in both views.

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
