# Configuration reference

uah reads its configuration from TOML files and combines it with flags, the environment, and a resumed session. This page lists every key: its type, default, meaning, which files may set it, and how a project file merges with the user file. `uah config` shows the effective value of each key for a workspace and where it came from.

1. [Files](#files)
2. [Precedence](#precedence)
3. [Keys](#keys): [model and engine](#model-and-engine), [sandbox and approvals](#sandbox-and-approvals), [review](#review), [compaction](#compaction), [instructions and skills](#instructions-and-skills), [hooks](#hooks), [MCP servers](#mcp-servers), [TUI](#tui), [projects](#projects)
4. [Environment variables](#environment-variables)
5. [uah config](#uah-config)
6. [Examples](#examples)

## Files

The configuration directory is `$XDG_CONFIG_HOME/uagent`, or `~/.config/uagent` when `XDG_CONFIG_HOME` is unset. The names say `uagent` because uah shares uagent's directories.

| File | What it holds | Applies when |
| --- | --- | --- |
| `<config dir>/config.toml` | The user file: every key | Always. `--config` or `UAGENT_CONFIG` names another file |
| `<workspace>/.uagent/config.toml` | The project file: every key except `[projects]` | The user file has `[projects."<workspace>"]` with `trusted = true`. The path is the absolute workspace path, symlinks not resolved |
| `<config dir>/rules/*.rules` | The user's command rules (Starlark `prefix_rule`); "don't ask again" appends to `default.rules` | Always |
| `<workspace>/.uagent/rules/*.rules` | The project's command rules | The workspace is trusted, with or without a project file |
| `<config dir>/trusted-hooks.json` | The project hook commands `uah hooks trust` approved, by SHA-256 | Written by uah; do not edit |
| `<config dir>/mcp-credentials.json` | MCP OAuth logins from `uah mcp login`, readable only by you (0600) | Written by uah when `mcp_oauth_credentials_store` is `file`, or `auto` without a usable OS keyring; do not edit |
| `<config dir>/AGENTS.md`, `$CODEX_HOME/AGENTS.md` | User instructions; see [Instructions and skills](../README.md#instructions-and-skills) | Unless `--no-instructions` or `[instructions] enabled = false` |
| `<config dir>/skills`, `$CODEX_HOME/skills`, `.agents/skills` | Skills; see [Instructions and skills](../README.md#instructions-and-skills) | Embedded engine |

A missing file is not an error. An unknown key is, in either file, so a typo fails loudly. A project file with a `[projects]` table is an error.

## Precedence

Each value comes from the first of these that sets it:

1. A flag.
2. The flag's environment variable (see [Environment variables](#environment-variables)).
3. The resumed session (`--session`, `uah resume`, `uah run --last`): only the provider, model, effort, and workspace.
4. The project file.
5. The user file.
6. The default.

The exceptions, as the code applies them:

- A `--provider` flag that changes the provider, compared with the resumed session's, else the configured one, else openai-codex, drops the resumed and configured models. The model is then `--model`, or the provider's default: `gpt-6-sol` for openai-codex and none for the others.
- The workspace comes from `-C`, the resumed session, or the current directory; no file sets it.
- `--timeout` (30m) and `--max-disk` (5G) have defaults, but a default counts only when the flag is not given: the files come first.
- `--fast` given, even as `--fast=false`, wins. Otherwise `fast` is on when either file turns it on.
- `--no-instructions` turns instructions off whatever the files say; no flag turns them on over `enabled = false`.
- `project_doc_max_bytes`, from either file, wins over `[instructions] max_bytes` from either file.
- Keys without a flag come only from the files and the defaults.

A project file merges into the user file key by key, in one of four ways. The key tables below name the way for each key.

| Merge | Rule |
| --- | --- |
| override | The project value replaces the user value when the project file sets it. An empty string, 0, or false in the project file counts as unset, except for the keys marked "can unset", whose explicit value always replaces |
| append | The project list follows the user list. For `[shell_environment_policy] set`, the project's variables are added and replace the user's of the same name |
| OR | True when either file says true; the project file cannot turn it off |
| replace by name | A project `[mcp_servers.<name>]` replaces the user's server of the same name whole; other servers stay |

## Keys

Every key may be set in the user file and in a trusted project file, except `[projects]`, which is user-file only.

### Model and engine

| Key | Type | Default | Flag, env | Merge | Meaning |
| --- | --- | --- | --- | --- | --- |
| `provider` | string | `openai-codex` | `--provider`, `UNREAL_HARNESS_LLM_PROVIDER` | override | The LLM provider: openai, openai-codex, openrouter, fireworks, or ollama |
| `model` | string | `gpt-6-sol` on openai-codex, else none | `-m`, `--model`, `UNREAL_HARNESS_LLM_MODEL` | override | The model ID |
| `effort` | string | `high` | `-e`, `--effort` | override | The thinking level: low, medium, high, xhigh, or max |
| `timeout` | duration | `30m` | `-t`, `--timeout` | override | The wall-clock limit per run, as Go durations (`90s`, `1h`); `0s` disables it |
| `max_disk` | size | `5G` | `--max-disk` | override | Stop a run when tool output exceeds this size (`500M`, `5G`, bytes without a suffix); `0` disables it |
| `engine` | string | `embedded` | `--engine`, `UAH_ENGINE` | override | `embedded` runs the runner's packages in process; `process` spawns unreal-agent-runner ([engines](../README.md#engines)) |
| `fast` | bool | false | `--fast` | OR | Priority processing (`service_tier = "priority"`); needs the embedded engine and the openai or openai-codex provider |

### Sandbox and approvals

| Key | Type | Default | Flag, env | Merge | Meaning |
| --- | --- | --- | --- | --- | --- |
| `sandbox_mode` | string | `workspace-write` | `--sandbox`, `UAH_SANDBOX` | override | `read-only`, `workspace-write`, or `danger-full-access` (no sandbox), as Codex names them |
| `approval_policy` | string | `on-request` | `--ask`, `UAH_ASK` | override | Who answers an escalation or a `prompt` rule: `on-request` asks the user (headless runs deny), `never` denies. In a file, Codex's `on-failure` means `on-request` |
| `approvals_reviewer` | string | `auto_review` | none | override | `auto_review` lets the auto-reviewer judge before anyone is asked; `user` skips it |

`[sandbox_workspace_write]` configures the `workspace-write` mode:

| Key | Type | Default | Merge | Meaning |
| --- | --- | --- | --- | --- |
| `network_access` | bool | false | OR | Sandboxed commands may use the network |
| `writable_roots` | list of strings | `[]` | append | More writable directories. `~` is the home directory; relative paths are relative to the workspace |

`[approvals]` lists command prefixes, such as `"git status"`, besides the rules files:

| Key | Type | Default | Merge | Meaning |
| --- | --- | --- | --- | --- |
| `allow` | list of strings | `[]` | append | Commands that start with one of these run outside the sandbox without asking |
| `forbid` | list of strings | `[]` | append | Commands that start with one of these never run |

`[shell_environment_policy]` is Codex's filter for the environment commands get. Empty, commands get the whole environment:

| Key | Type | Default | Merge | Meaning |
| --- | --- | --- | --- | --- |
| `inherit` | string | `all` | override | The starting set: `all`, `core` (Codex's basic variables), or `none` |
| `ignore_default_excludes` | bool | true | override, can unset | `false` drops variables whose names match `*KEY*`, `*SECRET*`, or `*TOKEN*` |
| `exclude` | list of patterns | `[]` | append | Variables to drop. Patterns ignore case; `*` matches any run of characters and `?` one |
| `include_only` | list of patterns | `[]` | append | When set, only matching variables stay, applied last |
| `set` | table of strings | `{}` | append | Variables to set, after `inherit`, the default excludes, and `exclude` |

### Review

`[review]` configures the auto-reviewer's model call ([approvals](../README.md#approvals-and-rules)):

| Key | Type | Default | Merge | Meaning |
| --- | --- | --- | --- | --- |
| `model` | string | `codex-auto-review` on openai-codex, else the session's model | override | The review model, on the session's provider |
| `effort` | string | `low` | override | The review effort: low, medium, high, xhigh, or max |
| `timeout` | duration | `90s` | override | The limit for one review; a review that times out denies |

### Compaction

| Key | Type | Default | Merge | Meaning |
| --- | --- | --- | --- | --- |
| `auto_compact_percent` | integer 0–100 | 90 | override, can unset | Compact before a model request once the context in use (the last response's tokens plus an estimate of what was added since) reaches this share of the context window; 0 turns automatic compaction off |
| `model_context_window` | integer | the model table (272,000 for current and unknown models) | override | The context window in tokens, for compaction and the context meter |

### Instructions and skills

| Key | Type | Default | Flag | Merge | Meaning |
| --- | --- | --- | --- | --- | --- |
| `project_doc_fallback_filenames` | list of strings | `[]` | none | override, can unset | File names to read in a directory without `AGENTS.override.md` or `AGENTS.md`; `["CLAUDE.md"]` reads Claude Code's files |
| `project_root_markers` | list of strings | `[".git"]` | none | override, can unset | Names that mark the project root, where discovery starts; `[]` reads the workspace only |
| `project_doc_max_bytes` | integer | 32768 | none | override | The size cap for all instruction files together; wins over `[instructions] max_bytes` |

`[instructions]`:

| Key | Type | Default | Flag | Merge | Meaning |
| --- | --- | --- | --- | --- | --- |
| `enabled` | bool | true | `--no-instructions` turns it off | override, can unset | Load AGENTS.md files into the system prompt |
| `max_bytes` | integer | 32768 | none | override | The size cap, when `project_doc_max_bytes` is unset |

Discovery order and the skill folders are in the README's [Instructions and skills](../README.md#instructions-and-skills).

### Hooks

Each `[[hooks.<Event>]]` entry runs a command at an event. The events are `SessionStart`, `SessionEnd`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `Stop`, `SubagentStop`, `PreCompact`, and `PermissionRequest`; the contract is in the README's [Hooks](../README.md#hooks).

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `matcher` | string | every tool | A regular expression that must match the whole tool name, for the tool events (`PreToolUse`, `PostToolUse`, `PermissionRequest`) |
| `command` | string | required | The command, run with `/bin/sh -c` in the workspace |
| `timeout` | duration | `60s` | The limit for one run of the hook |

Merge: append, per event, the user file's hooks first. Project hooks run only after `uah hooks trust` records their exact commands.

### MCP servers

Each `[mcp_servers.<name>]` table is one server, in Codex's format, so a Codex section copies over ([MCP servers](../README.md#mcp-servers)). Merge: replace by name. A server has `command` (stdio) or `url` (streamable HTTP), not both.

| Key | Type | Default | Transport | Meaning |
| --- | --- | --- | --- | --- |
| `command` | string | none | stdio | The program to start |
| `args` | list of strings | `[]` | stdio | Its arguments |
| `env` | table of strings | `{}` | stdio | Variables to set; the server otherwise gets only basic variables such as `HOME` and `PATH` |
| `env_vars` | list of strings | `[]` | stdio | Variables passed through from uah's environment by name |
| `cwd` | string | the workspace | stdio | The server's working directory |
| `url` | string | none | HTTP | The server's endpoint |
| `bearer_token_env_var` | string | none | HTTP | The variable holding a bearer token |
| `http_headers` | table of strings | `{}` | HTTP | Headers to send |
| `env_http_headers` | table of strings | `{}` | HTTP | Headers whose values come from the named variables |
| `enabled` | bool | true | both | `false` does not start the server |
| `required` | bool | false | both | A run fails when the server does not start, instead of leaving it out |
| `startup_timeout_sec` | number | 30 | both | Seconds to wait for the server to start |
| `startup_timeout_ms` | integer | none | both | The same in milliseconds, when `startup_timeout_sec` is unset |
| `tool_timeout_sec` | number | 300 | both | Seconds a tool call may take |
| `enabled_tools` | list of strings | all | both | When set, only these tools are offered |
| `disabled_tools` | list of strings | `[]` | both | Tools not offered |
| `supports_parallel_tool_calls` | bool | false | both | Calls may overlap; otherwise they run one at a time |
| `default_tools_approval_mode` | string | `auto` | both | The approval mode for tools without their own |
| `tools` | tables | none | both | Per-tool settings, by tool name, below |

`[mcp_servers.<name>.tools.<tool>]` sets one tool's `approval_mode`: `auto` (ask unless the annotations say read-only, or non-destructive and closed-world), `prompt` (always ask), `writes` (ask unless read-only), or `approve` (never ask).

OAuth for streamable HTTP servers, with Codex's keys. A server that answers 401 and advertises OAuth needs `uah mcp login <name>`; until then it shows "needs login" in `/mcp` and `uah doctor`. A server with `bearer_token_env_var` or an `Authorization` header never uses OAuth.

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `auth` | string | `oauth` | How uah authorizes; only `oauth` is supported (Codex's `chatgpt` and `ema_auth` need a Codex account) |
| `scopes` | list of strings | the scopes the server advertises | The scopes `uah mcp login` asks for; `--scopes` replaces them |
| `oauth_resource` | string | the server's own | The RFC 8707 resource sent with the authorization and token requests |
| `oauth.client_id` | string | none: uah registers a client dynamically | A client registered with the authorization server ahead of time, in `[mcp_servers.<name>.oauth]` |
| `oauth.callback_url` | string | `http://127.0.0.1:<port>/callback` | The redirect URI sent to the server; the listener still binds 127.0.0.1. Overrides `mcp_oauth_callback_url` |
| `oauth.callback_port` | integer | a port the OS picks | The listener's port. Overrides `mcp_oauth_callback_port` |

Top-level OAuth keys, merged as override:

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `mcp_oauth_credentials_store` | string | `auto` | Where logins are kept: `keyring` (the OS keyring, service "uah MCP Credentials"), `file` (`<config dir>/mcp-credentials.json`, 0600), or `auto` (the keyring, else the file, as Codex does) |
| `mcp_oauth_callback_port` | integer | a port the OS picks | The port `uah mcp login` listens on for the browser's redirect |
| `mcp_oauth_callback_url` | string | `http://127.0.0.1:<port>/callback` | The redirect URI sent to the authorization server, for a callback that reaches 127.0.0.1 some other way |

Codex keys uah does not support are errors: `bearer_token`, `http_headers_helper`, `environment_id`, `omit_tools_from`, `oauth.authorization_server_issuer`, `tools.<tool>.output_token_limit`, `env_vars` entries written as tables, and the top-level `tool_output_token_limit`.

### Subagents

`[agents]` (embedded engine; [README](../README.md#subagents)):

| Key | Type | Default | Merge | Meaning |
| --- | --- | --- | --- | --- |
| `enabled` | bool | true | override, can unset | Offer `spawn_agent`, `send_input`, `wait`, and `close_agent` |
| `max_concurrent_threads_per_session` | int | 4 | override | Open subagents per session tree; Codex's `max_threads` is an alias |
| `max_threads` | int | none | override | Codex's older name for `max_concurrent_threads_per_session` |
| `max_depth` | int | 1 | override | How deep subagents nest; 1 means subagents cannot spawn their own |
| `default_subagent_model` | string | the parent's model | override | Model for subagents a role or call does not set |
| `default_subagent_reasoning_effort` | string | the parent's effort | override | Effort for subagents a role or call does not set |

Roles are Codex role files in `~/.config/uagent/agents/*.toml` and, for a trusted workspace, `<workspace>/.uagent/agents/*.toml` (a project role replaces a user role of the same name). uah reads `name`, `description`, `nickname_candidates`, `model`, `model_reasoning_effort`, and `developer_instructions`.

### TUI

`[tui]`:

| Key | Type | Default | Merge | Meaning |
| --- | --- | --- | --- | --- |
| `details` | bool | false | OR | Start in the detailed view; ctrl+t toggles it |

### Projects

`[projects."<absolute workspace path>"]`, in the user file only:

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `trusted` | bool | false | Apply the workspace's `.uagent/config.toml` and `.uagent/rules/*.rules` |

## Environment variables

| Variable | Flag | Config key | Meaning |
| --- | --- | --- | --- |
| `UNREAL_HARNESS_LLM_PROVIDER` | `--provider` | `provider` | The provider |
| `UNREAL_HARNESS_LLM_MODEL` | `--model` | `model` | The model |
| `UNREAL_HARNESS_LLM_BASE_URL` | `--base-url` | none | The LLM base URL |
| `UAH_ENGINE` | `--engine` | `engine` | The engine |
| `UAH_SANDBOX` | `--sandbox` | `sandbox_mode` | The sandbox mode |
| `UAH_ASK` | `--ask` | `approval_policy` | The approval policy |
| `UAGENT_CONFIG` | `--config` | none | The user file |
| `UAGENT_STATE_DIR` | `--state-dir` | none | Sessions, logs, and run records |
| `UAGENT_RUNNER` | `--runner` | none | unreal-agent-runner, for the process engine |
| `XDG_CONFIG_HOME` | none | none | The parent of the configuration directory |
| `CODEX_HOME` | none | none | Codex's directory (`~/.codex`) for `AGENTS.md` and skills |
| `BROWSER` | none | none | The program `uah mcp login` opens the authorization URL with, instead of the system's opener |

## uah config

`uah config` takes the session flags and shows what a session started with them would use: the workspace, the files read, and one line per key with its value and source (`flag`, `env`, `session`, `project file`, `user file`, or `default`). Keys that append or OR list every file that set them, such as `user file + project file`. `--json` prints the same as JSON. A flag equal to its environment variable's value is reported as `env`.

```sh
uah config                     # the current directory
uah config -C ~/code/proj      # another workspace
uah config --session 3f2a      # as resuming a session would
uah config --json | jq '.settings[] | select(.sources != ["default"])'
```

## Examples

A complete user file, `~/.config/uagent/config.toml`:

```toml
provider = "openai-codex"
model = "gpt-6-sol"
effort = "high"
timeout = "30m"                    # per run; "0s" disables
max_disk = "5G"                    # tool output per run; "0" disables
engine = "embedded"                # or "process"
fast = false                       # priority processing
sandbox_mode = "workspace-write"   # read-only, workspace-write, danger-full-access
approval_policy = "on-request"     # or never
approvals_reviewer = "auto_review" # or user: skip the auto-reviewer
auto_compact_percent = 90          # 0 turns automatic compaction off
model_context_window = 272000      # tokens; overrides the model table
project_doc_fallback_filenames = ["CLAUDE.md"]   # also read Claude Code's files
project_root_markers = [".git"]
project_doc_max_bytes = 32768

[instructions]
enabled = true
max_bytes = 32768                  # used when project_doc_max_bytes is unset

[sandbox_workspace_write]
network_access = false
writable_roots = ["~/Library/Caches/go-build"]

[approvals]
allow = ["go test", "git status"]  # run outside the sandbox without asking
forbid = ["git push --force"]      # never run

[shell_environment_policy]
inherit = "all"                    # all, core, none
ignore_default_excludes = true     # false drops *KEY*, *SECRET*, *TOKEN*
exclude = ["AWS_*"]
include_only = []
set = { CI = "1" }

[review]
model = "codex-auto-review"
effort = "low"
timeout = "90s"

[tui]
details = false

[[hooks.PreToolUse]]               # embedded engine only
matcher = "Bash"                   # the whole tool name, as a regular expression
command = "~/.config/uagent/hooks/no-rm-rf.sh"
timeout = "10s"                    # default 60s

[[hooks.Stop]]
command = "osascript -e 'display notification \"uah is idle\"'"

[mcp_servers.docs]                 # stdio
command = "npx"
args = ["-y", "@example/docs-mcp"]
env = { DOCS_LANG = "en" }
env_vars = ["DOCS_TOKEN"]
cwd = "/tmp"
enabled = true
required = false
startup_timeout_sec = 20
tool_timeout_sec = 60
disabled_tools = ["delete_page"]
supports_parallel_tool_calls = false
default_tools_approval_mode = "auto"

[mcp_servers.docs.tools.search]
approval_mode = "approve"

[mcp_servers.tracker]              # streamable HTTP
url = "https://mcp.example.com/mcp"
bearer_token_env_var = "TRACKER_TOKEN"
http_headers = { "X-Team" = "core" }
env_http_headers = { "X-Org" = "TRACKER_ORG" }
enabled_tools = ["search", "get_issue"]
startup_timeout_ms = 20000

[mcp_servers.linear]               # streamable HTTP with OAuth: `uah mcp login linear`
url = "https://mcp.linear.app/mcp"
scopes = ["read"]

[projects."/Users/me/code/proj"]
trusted = true                     # apply proj/.uagent/config.toml and its rules
```

A project file, `/Users/me/code/proj/.uagent/config.toml`, applied because the user file trusts the workspace:

```toml
effort = "medium"                  # override: replaces the user's "high"
auto_compact_percent = 0           # override, can unset: no automatic compaction here
fast = true                        # OR: cannot turn the user's fast off

[sandbox_workspace_write]
writable_roots = ["../shared"]     # append: after the user's roots, relative to the workspace

[approvals]
allow = ["make test"]              # append: the user's allow list plus this

[[hooks.PreToolUse]]               # append; runs only after `uah hooks trust`
matcher = "Bash"
command = ".uagent/hooks/guard.sh"
timeout = "5s"

[mcp_servers.docs]                 # replace by name: the user's docs server is not used here
url = "https://docs.internal.example.com/mcp"
```

This repository's own [.uagent/config.toml](../.uagent/config.toml) is a working project file.
