# MCP: plan

Status: decided and built 2026-09-24 (ledger item 5); validated and hardened, with OAuth, `uah mcp`, and the `/mcp` panel, the same day. Codex facts are from openai/codex at rust-v0.156.1 (`codex-rs/`); runner facts are from unreal-agent v0.1.1 (`RN/`); SDK facts are from `github.com/modelcontextprotocol/go-sdk` v1.8.0.

The rule, as for the sandbox: do what Codex does, unless the runner forces a difference.

1. [How Codex does it](#how-codex-does-it)
2. [How MCP tools enter the runner](#how-mcp-tools-enter-the-runner)
3. [Packages and files](#packages-and-files)
4. [Configuration](#configuration)
5. [OAuth](#oauth)
6. [Validation](#validation)
7. [Open decisions](#open-decisions)

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
| OAuth keys | Per server: `auth` (`oauth` default), `scopes`, `oauth_resource`, `[oauth]` with `client_id`, `callback_url`, `callback_port`. Top level: `mcp_oauth_credentials_store` (`auto`, `file`, `keyring`), `mcp_oauth_callback_port`, `mcp_oauth_callback_url` | `config/src/mcp_types.rs`, `config/src/config_toml.rs` |
| Tokens | The `keyring` crate, service "Codex MCP Credentials"; `auto` tries the keyring and falls back to `CODEX_HOME/.credentials.json` (0600). Key: the server name, `\|`, and 16 hex digits of a SHA-256 of `{"type":"http","url":…,"headers":{}}`. Refreshed 30 s before expiry and saved back | `rmcp-client/src/oauth.rs` |
| Login | A loopback listener on 127.0.0.1 (a port the OS picks unless configured), path `/callback`; PKCE S256; dynamic client registration (or the configured `client_id`); prints "Authorize `<server>` by opening this URL in your browser:" and opens it; waits 300 s | `rmcp-client/src/perform_oauth_login.rs` |
| Auth status | `Unsupported`, `Not logged in`, `Bearer token`, `OAuth`: a bearer token first, then stored tokens, then metadata discovery (5 s) | `rmcp-client/src/auth_status.rs` |
| CLI | `codex mcp list/get/add/remove/login/logout`; `add` edits the file with `toml_edit`, keeping its formatting | `cli/src/mcp_cmd.rs` |
| `/mcp` | A line per server (`connected`, `starting`, `authentication required`, `failed`, `disabled`) with its tool count; `/mcp verbose` adds auth and tools | `tui/src/history_cell/mcp.rs` |
| Output | Tool output is cut to the model's budget (10,000 tokens) with a middle marker | `utils/output-truncation` |
| List changes | `notifications/tools/list_changed` is only logged; the tool list stays | `rmcp-client/src/logging_client_handler.rs` |
| Restarts and stderr | A crashed server is not restarted; an HTTP session that expired (404) is initialized again. Standard error is logged line by line ("MCP server stderr"), never shown | `codex-mcp`, `rmcp-client/src/stdio_server_launcher.rs` |

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
| `internal/mcp/manager.go` | The `Manager`: starting servers once, the tools to offer, closing; `Tool.NeedsApproval` |
| `internal/mcp/server.go` | One server's connection: connecting within the startup timeout, watching for a stop, reconnecting an expired HTTP session, naming tools |
| `internal/mcp/call.go` | Calls: one at a time unless parallel, the tool timeout, one retry after a session expired |
| `internal/mcp/status.go` | `ServerStatus` for `/mcp` and `uah doctor` |
| `internal/mcp/result.go` | Converts a `CallToolResult` to text and images |
| `internal/mcp/transport.go` | Stdio commands with Codex's environment, and HTTP headers |
| `internal/mcp/stderr.go` | Stdio servers' standard error, logged a line at a time |
| `internal/mcp/auth.go` | Auth status, OAuth discovery, and the running server's OAuth handler (stored tokens, refresh, "needs login") |
| `internal/mcp/login.go`, `callback.go` | `uah mcp login` through the SDK's authorization code handler, and the loopback callback |
| `internal/mcp/credentials.go` | Stored logins: the OS keyring, a 0600 file, or both |
| `internal/mcp/configfile.go` | `uah mcp add` and `remove`: editing the user file and keeping the rest of it |
| `internal/engine/embedded/mcptool.go` | The registry wrapper, the translator, and the approval gate |
| `internal/engine/embedded/mcpjobs.go` | The remote job handler that runs calls |
| `internal/app/mcp.go`, `mcpcli.go` | The manager and the credential store from the configuration; the `uah mcp` operations |
| `cmd/uah/mcp.go`, `mcpprint.go` | `uah mcp` and its output |
| `internal/tui/state/mcp.go`, `internal/tui/render/mcp.go` | `/mcp` and `/mcp verbose`, and the panel |
| `testing/mcpserver`, `testing/oauthserver` | A stdio test server, and an MCP server behind a small OAuth authorization server |
| `internal/config/config.go` | `mcp_servers` and the top-level OAuth keys, merged from the project file |
| `internal/engine/engine.go` | `MCPLister`, the optional engine interface behind `/mcp` |

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

Supported keys, all with Codex's names and meaning: `command`, `args`, `env`, `env_vars` (names only), `cwd`, `url`, `bearer_token_env_var`, `http_headers`, `env_http_headers`, `enabled`, `required`, `startup_timeout_sec`, `startup_timeout_ms`, `tool_timeout_sec`, `enabled_tools`, `disabled_tools`, `supports_parallel_tool_calls`, `default_tools_approval_mode`, `tools.<tool>.approval_mode`, and for OAuth `auth` (only `oauth`), `scopes`, `oauth_resource`, `oauth.client_id`, `oauth.callback_url`, `oauth.callback_port`, with the top-level `mcp_oauth_credentials_store`, `mcp_oauth_callback_port`, and `mcp_oauth_callback_url`. Codex keys uah does not support (`bearer_token`, `http_headers_helper`, `environment_id`, `omit_tools_from`, `oauth.authorization_server_issuer`, `tools.<tool>.output_token_limit`, `tool_output_token_limit`, and `env_vars` entries written as tables) are unknown-key errors, so nothing is silently ignored.

A trusted project file may add servers; a project server with the name of a user server replaces it whole.

## OAuth

uah does the OAuth work through the SDK (`auth.AuthorizationCodeHandler`, `oauthex`, and `golang.org/x/oauth2`); it adds only the pieces the SDK leaves to its caller: the browser step, the loopback listener, and storage.

- **Login** (`uah mcp login <name>`). `mcp.Login` connects to the server with the SDK's authorization code handler as the transport's OAuth handler. The server's 401 starts the SDK's flow: protected resource metadata (RFC 9728) from the `WWW-Authenticate` challenge or the well-known places, authorization server metadata, the client (the configured `oauth.client_id`, else dynamic registration as "uah", a public client with `token_endpoint_auth_method` `none`), PKCE S256, and the code exchange. uah's part: a listener on 127.0.0.1 (`/callback`, a port the OS picks unless configured), printing "Authorize `<server>` by opening this URL in your browser:" and the URL, opening it (`$BROWSER`, else `open` or `xdg-open`; `--no-browser` only prints), and waiting 300 s. Scopes are `--scopes`, else `scopes`, else what the server advertises, with `offline_access` when the server offers it, so a refresh token comes back. `oauth_resource` replaces the RFC 8707 resource in the authorization URL and the token request. The tokens are saved as the SDK hands them over, and saved again when the SDK's first read refreshes them.
- **Storage.** Codex keeps tokens in the OS keyring and falls back to a file, so uah does the same with `github.com/zalando/go-keyring` (maintained; the macOS keychain through `/usr/bin/security` with the secret on standard input, Secret Service on Linux, the Windows credential manager): service "uah MCP Credentials", one JSON entry per server under Codex's key (the name, `|`, and 16 hex digits of a SHA-256 of the URL's identity), so a changed URL needs a new login. `auto` (the default) falls back to `<config dir>/mcp-credentials.json` (0600, replaced atomically) when the keyring fails or refuses an entry, such as one too large for the macOS keychain; `file` and `keyring` pick one. An entry holds the client (ID, secret if any, token URL, auth style, scopes) and the tokens with their expiry in milliseconds, as Codex's.
- **Running.** Each HTTP server without a bearer token or `Authorization` header gets a `storedAuth` handler: it sends the stored token through the oauth2 package, which refreshes it when it is within 10 s of expiring; each new token is saved back. A 401 (or a 403 asking for more scopes) never opens a browser: when the server advertises OAuth the server's state becomes `needs_login` with Codex's message ("The `<server>` MCP server is not logged in. Run `uah mcp login <server>`.", or "requires OAuth reauthentication" when a stored login was rejected), shown by `/mcp`, `uah doctor` (a warning, a failure for a `required` server), and `uah mcp list` (Auth "Not logged in").
- **Status** without starting a server (`uah mcp list`, `get`): a bearer token, then a usable stored login, then discovery (5 s): "Bearer token", "OAuth", "Not logged in", or "Unsupported", as Codex's.

## Validation

Each area was checked against the code and, where it could fail, a test with a real server (`testing/mcpserver` over stdio, an in-process SDK streamable HTTP server, `testing/oauthserver`). "Fixed" findings changed code; the tests run with `-race -count=20`.

| Area | Finding | Result |
| --- | --- | --- |
| Server crash | The server is marked failed and later calls return "the MCP server `<name>` failed: the server stopped"; no restart, as Codex. Closing the session then reported the crash again as "failed to close the MCP server: exit status 3" | Fixed: a server that stopped on its own is not reported again on close (`TestCrashingServer`) |
| Restart policy | Codex restarts only its own apps server and re-initializes expired HTTP sessions | Same; see HTTP sessions |
| Slow startup | Bounded by `startup_timeout_sec`; the others start meanwhile; the error now says a slow server needs a larger `startup_timeout_sec`. The SDK closes a timed-out server's standard input and stops it after 5 s | OK (`TestStartupFailures`) |
| Many tools | Every page of `tools/list` is read | OK: 1,511 tools (`TestManyToolsAndListChanges`) |
| Huge results | The runner bounds a remote job's text to 40,000 characters, head and tail with "…N bytes truncated…", as Codex bounds tool output to about 10,000 tokens; images are not cut, as in Codex. The SDK caps one stdio message at 16 MiB; a larger one breaks the connection and the server shows as failed | OK (`TestLargeResult`, `TestEmbedded_MCPBoundsOutputAndChecksArguments`); the 16 MiB cap is the SDK's |
| Arguments that are not a JSON object | The user was asked to approve the call before the arguments were checked, then it was refused | Fixed: checked first; nothing is asked (`TestEmbedded_MCPBoundsOutputAndChecksArguments`) |
| Calls to a server without parallel calls | The tool timeout started while a call waited for the previous one, so a queued call could time out before it ran | Fixed: the wait is bounded only by cancellation, the timeout starts when the call runs (`TestSerialCallsWaitTheirTurn`) |
| Cancellation | A call canceled mid-call sends `notifications/cancelled` through the SDK and reports canceled; one canceled while waiting its turn never reaches the server | OK (`TestEmbedded_MCPInterruptThenContinue`, `TestSerialCallsWaitTheirTurn`) |
| Resume with calls in flight | A job found awaiting after a restart fails as interrupted instead of calling twice; stored results need no server | OK (`TestMCPJobs_InterruptedCallIsNotRepeated`, `TestEmbedded_MCPResumesWithoutTheServer`) |
| Standard error | A stdio server's standard error was copied raw to the log output: the terminal's standard error under `uah run`, unlabeled in the TUI's log | Fixed: logged a line at a time as "MCP server stderr" with the server's name at info level, as Codex; `uah run` shows it only with `--log-level info` (`TestStderrIsLogged`) |
| Environment | Only Codex's basic variables, then `env_vars`, then `env` | OK (`TestServerEnvironment`) |
| HTTP errors and timeouts | A 500 at startup fails the server with the status; a server that goes away fails the call within the tool timeout. A server that forgot its session (404) left the server unusable for the rest of the session | Fixed: a new session and one retry, as Codex; the call never ran on the forgotten session (`TestHTTPFailures`) |
| HTTP 401 | A server that wants OAuth failed with "Unauthorized" | Built: "needs login" (see [OAuth](#oauth); `TestOAuth`) |
| Tool list changes | Codex does not refresh on `notifications/tools/list_changed`; uah ignored it silently | Logged now; the startup list stays (`TestManyToolsAndListChanges`); see Open decisions |
| Name collisions | Names that sanitize alike get Codex's hash suffix, stably | OK (`TestNames`, `TestNameCollisions`) |
| `/mcp` while starting | A ready server showed 0 tools until every server had started | Fixed: named as far as known (`status.go`) |

## Open decisions

Each has the default taken.

1. **Approval (resolved after the merge).** `prompt`, `writes` for tools that are not read-only, and `auto` by Codex's annotation rule (`requires_mcp_tool_approval`) now ask through the session's approval prompt, and PermissionRequest hooks can answer. Headless runs and `approval_policy = "never"` refuse with a reason.
2. **Denying a tool.** Codex has no `deny` approval mode; `disabled_tools` hides a tool from the model and refuses calls to it. uah does the same and adds nothing.
3. **Sandbox.** MCP servers run outside the command sandbox, as in Codex. Default taken: no sandbox for servers.
4. **When servers start.** On the first run or `/mcp`, not at session open, because `uah` builds an engine to validate flags before the TUI opens. Default taken: lazy start.
5. **Parallel calls.** Codex serializes calls to a server unless `supports_parallel_tool_calls`; uah does the same per server.
6. **Resources and prompts.** Not built (the ledger's Later list). The seam: `Manager.open` keeps the session; a resources or prompts feature adds a lister beside `Tools` and its own remote job plan type beside `uah.mcp_call`.
7. **Process engine.** MCP needs the embedded engine; the process engine shows a notice when servers are configured, as it does for PreToolUse hooks.
8. **Name length.** Codex allows 128 bytes because it sends MCP tools in Responses API namespaces; uah sends flat function names, which the API limits to 64 characters, so uah cuts at 64 with Codex's hash suffix.
9. **Closing.** The SDK closes a stdio server by closing its stdin and waits up to 5 seconds before SIGTERM, and waits for calls in flight; a server that ignores a canceled call can delay closing a session that long.
10. **Restarts.** A crashed stdio server stays failed for the session, as in Codex; `/new` starts it again. Default taken: no automatic restart.
11. **Tool list changes.** Logged, not applied, as in Codex, so the tools offered (and the prompt cache) stay stable within a session. Default taken: Codex's.
12. **Login inside a session.** After `uah mcp login`, a running session keeps the server in "needs login" until `/new` or a restart; the `/mcp` panel says so. Default taken: no reconnect on `/mcp`.
13. **`--no-browser`.** Codex's prompts for the callback URL to be pasted; uah's prints the URL and still waits on the loopback listener, which works when the browser can reach 127.0.0.1 (the same machine, or a forwarded port). Default taken: no paste prompt.
14. **Refresh margin.** Codex refreshes 30 s before expiry; uah uses the oauth2 package's 10 s. Default taken: the library's.
15. **`add` and OAuth.** Codex's `mcp add --url` starts the login when the server advertises OAuth. uah does so only on a terminal; otherwise it prints `uah mcp login <name>`. Default taken: this.
16. **Unsupported OAuth pieces.** Client ID metadata documents (Codex's CIMD at chatgpt.com), `auth = "chatgpt"` and `"ema_auth"`, and `oauth.authorization_server_issuer` need Codex's account or hosting. Default taken: not supported; they are errors.
