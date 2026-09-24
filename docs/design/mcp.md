# MCP: plan

Status: decided 2026-09-24, built in the same change. Codex facts are from openai/codex at rust-v0.156.1 (`codex-rs/`); runner facts are from unreal-agent v0.1.1 (`RN/`).

The rule, as for the sandbox: do what Codex does, unless the runner forces a difference.

1. [How Codex does it](#how-codex-does-it)
2. [How MCP tools enter the runner](#how-mcp-tools-enter-the-runner)
3. [Packages and files](#packages-and-files)
4. [Configuration](#configuration)
5. [Open decisions](#open-decisions)

## How Codex does it

| Topic | Codex | Where |
| --- | --- | --- |
| Configuration | `[mcp_servers.<name>]`: stdio `command`, `args`, `env`, `env_vars`, `cwd`; streamable HTTP `url`, `bearer_token_env_var`, `http_headers`, `env_http_headers`; shared `enabled`, `required`, `startup_timeout_sec` (or `startup_timeout_ms`), `tool_timeout_sec`, `enabled_tools`, `disabled_tools`, `supports_parallel_tool_calls`, `default_tools_approval_mode`, `[mcp_servers.<name>.tools.<tool>] approval_mode`. Unknown keys are errors | `config/src/mcp_types.rs` (`RawMcpServerConfig`) |
| Defaults | Startup timeout 30 s, tool timeout 300 s | `codex-mcp/src/rmcp_client.rs` |
| Child environment | Only `HOME`, `LOGNAME`, `PATH`, `SHELL`, `USER`, `LANG`, `LC_ALL`, `TERM`, `TMPDIR`, `TZ` (and `__CF_USER_TEXT_ENCODING`) from the parent, then `env_vars` by name, then `env` | `rmcp-client/src/utils.rs` |
| Tool filter | `enabled_tools` is an allow list when set; `disabled_tools` then removes names | `codex-mcp/src/tools.rs` (`ToolFilter`) |
| Tool names | `mcp__<server>__<tool>`, each part sanitized to `[A-Za-z0-9_]`, at most 128 bytes; a name that is too long or collides gets a `_` and 12 hex digits of a SHA-1 of the raw identity | `codex-mcp/src/tools.rs`, `codex-mcp/src/mcp/mod.rs` |
| Results | `structuredContent`, when present, replaces the content as JSON text; otherwise text parts become text and images become `data:` URLs (the MIME type is added when the data lacks it) | `protocol/src/models.rs` (`as_function_call_output_payload`) |
| Approval | `approval_mode` is `auto`, `prompt`, `writes`, or `approve`. `auto` asks for tools that are not read-only and are destructive or open-world (by annotations); `writes` asks unless the tool is read-only; `prompt` always asks; `approve` never asks | `core/src/mcp_tool_call.rs` |

## How MCP tools enter the runner

The runner's `tool.Registry` is an interface, and the coordinator uses only `Resolve(name)` to find a call's translator and the builder's tool list (filled from `StaticDefinitions`) to offer tools. So uah wraps the registry, as it already does for the sandbox and PreToolUse hooks:

- `StaticDefinitions` appends one definition per MCP tool: the qualified name, the server's description, and its input schema as the parameters.
- `Resolve` returns an MCP translator for any `mcp__` name. For a name no server offers now, `Translate` reports that it is not available, but `TranslateResult` still reads stored results, so a session with past MCP calls resumes after its server is removed.
- PreToolUse wraps the MCP registry, so hooks see `mcp__<server>__<tool>` and matchers apply.

A call runs as a runner **remote job**, the runner's mechanism for work outside the coordinator (`RN/harness/operation/remote_job.go`). `Translate` submits a `remote_job` operation whose plan (`uah.mcp_call`, version 1) holds the server, the raw tool name, and the arguments. The embedded engine passes an MCP `RemoteJobHandler` to `operation.NewLocalOperationManager`, as the runner's own `ToolFactory` passes `RemoteJobs`. The handler calls the tool on its own goroutine and reports `awaiting`, then `completed`, `failed`, or `canceled`, so the coordinator never waits on a server. The result text goes into `TerminalResult` (bounded like other tool output); images go into `Handle`, which the runner stores untouched, because truncating an image breaks it. A job found `awaiting` after a restart fails as interrupted instead of calling the tool twice.

Servers live as long as the session's engine: they start on the first run (or on `/mcp`), each bounded by its startup timeout, and stop when the session closes. A server that crashes fails its calls with an error result; it is not restarted.

## Packages and files

| File | Role |
| --- | --- |
| `internal/mcp/config.go` | `ServerConfig` in Codex's format, validation, timeouts, the tool filter, the approval mode |
| `internal/mcp/names.go` | Codex's qualified tool names |
| `internal/mcp/manager.go` | Starts servers (stdio and streamable HTTP, through `github.com/modelcontextprotocol/go-sdk`), lists tools, calls them with a timeout, reports status |
| `internal/mcp/result.go` | Converts a `CallToolResult` to text and images |
| `internal/engine/embedded/mcptool.go` | The registry wrapper, the translator, and the remote job handler |
| `internal/config/config.go` | `mcp_servers`, merged from the project file |
| `internal/engine/engine.go` | `MCPLister`, the optional engine interface behind `/mcp` |
| `internal/session`, `internal/tui/*` | `Session.MCPServers` and the `/mcp` command |

## Configuration

```toml
[mcp_servers.docs]
command = "npx"
args = ["-y", "@example/docs-mcp"]
env = { DOCS_LANG = "en" }
startup_timeout_sec = 20
tool_timeout_sec = 60
disabled_tools = ["delete_page"]

[mcp_servers.docs.tools.search]
approval_mode = "approve"

[mcp_servers.tracker]
url = "https://mcp.example.com/mcp"
bearer_token_env_var = "TRACKER_TOKEN"
```

Supported keys, all with Codex's names and meaning: `command`, `args`, `env`, `env_vars` (names only), `cwd`, `url`, `bearer_token_env_var`, `http_headers`, `env_http_headers`, `enabled`, `required`, `startup_timeout_sec`, `startup_timeout_ms`, `tool_timeout_sec`, `enabled_tools`, `disabled_tools`, `supports_parallel_tool_calls`, `default_tools_approval_mode`, and `tools.<tool>.approval_mode`. Codex keys uah does not support (`oauth`, `scopes`, `auth`, `bearer_token`, `http_headers_helper`, `environment_id`, `omit_tools_from`, `tools.<tool>.output_token_limit`, and `env_vars` entries written as tables) are unknown-key errors, so nothing is silently ignored.

A trusted project file may add servers; a project server with the name of a user server replaces it whole.

## Open decisions

Each has the default taken.

1. **Approval before item 2's approver merges.** `approve` runs the call. `prompt`, and `writes` for a tool that is not annotated read-only, need the user's approval: until the approver exists the call is refused with a message the model reads (as the sandbox refuses escalations). The hook point is `approvalFor` in `internal/engine/embedded/mcptool.go`; the main session connects it to the approver after the merge. `auto` (Codex's default) runs the call for now; with the approver it follows Codex's annotation rule. Default taken: this order.
2. **Denying a tool.** Codex has no `deny` approval mode; `disabled_tools` hides a tool from the model and refuses calls to it. uah does the same and adds nothing.
3. **Sandbox.** MCP servers run outside the command sandbox, as in Codex. Default taken: no sandbox for servers.
4. **When servers start.** On the first run or `/mcp`, not at session open, because `uah` builds an engine to validate flags before the TUI opens. Default taken: lazy start.
5. **Parallel calls.** Codex serializes calls to a server unless `supports_parallel_tool_calls`; uah does the same per server.
6. **Resources, prompts, OAuth, restarts.** Not built (the ledger's Out list).
7. **Process engine.** MCP needs the embedded engine; the process engine shows a notice when servers are configured, as it does for PreToolUse hooks.
