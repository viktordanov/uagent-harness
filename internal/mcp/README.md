<!-- memoria:section id="overview" files="manager.go config.go names.go" -->
# internal/mcp: MCP servers for the embedded engine

<!-- memoria:export id="summary" -->
uah runs the MCP servers in `[mcp_servers]` (Codex's format) on the embedded engine through the official Go SDK: stdio and streamable HTTP servers, their tools offered as `mcp__<server>__<tool>` and called without blocking the agent, Codex's approval modes, OAuth logins with `uah mcp login` kept in the OS keyring, and `uah mcp` to list, add, and remove servers.
<!-- /memoria:export -->

This package owns everything MCP that is not engine or UI wiring: the configuration, starting and watching servers, naming and calling tools, OAuth, stored logins, and editing the configuration file for `uah mcp add` and `remove`. The embedded engine, `internal/app`, `cmd/uah`, and the TUI use it through a small surface: `Manager` (`Tools`, `Call`, `Status`, `Close`), `Login`, `Logout`, `AuthStatusOf`, `AddServer`, and `RemoveServer`.

1. [Lifecycle of a server](#lifecycle-of-a-server)
2. [How a call flows](#how-a-call-flows)
3. [Approvals](#approvals)
4. [Authorization](#authorization)
5. [Configuration and Codex](#configuration-and-codex)
6. [Extending](#extending)

Behavior follows Codex (openai/codex rust-v0.156.1) unless the runner (unreal-agent v0.1.1) forces a difference; the decisions and the validation are in [the MCP design record](../../docs/design/mcp.md).
<!-- /memoria:section -->

<!-- memoria:section id="lifecycle" files="manager.go server.go status.go transport.go stderr.go" -->
## Lifecycle of a server

A `Manager` holds the configured servers and starts nothing until the first run, `/mcp`, or `uah doctor` asks (`Start`, `Tools`, `Status`). Each enabled server then connects on its own goroutine within its `startup_timeout_sec` (default 30 s) and lists its tools, every page. A server is `starting`, then `ready`, `failed`, or `needs_login`; `enabled = false` makes it `disabled`. `Tools` waits until every server has started or failed and names the ready servers' allowed tools once, so the list stays the same for the session. A `required` server that did not start fails the run.

A stdio server gets only Codex's basic variables (`HOME`, `PATH`, `USER`, and a few more), then `env_vars` by name, then `env`, and runs in `cwd` or the workspace. Its standard error is logged a line at a time ("MCP server stderr", with the server's name) and never reaches the screen. An HTTP server gets `http_headers`, `env_http_headers`, and the bearer token from `bearer_token_env_var`.

A goroutine watches each connection. When a stdio server exits or an HTTP server goes away, the server becomes `failed` and later calls fail with the reason; it is not restarted, as in Codex. An HTTP server that forgot its session (404) gets a new session on the next call. `notifications/tools/list_changed` is logged, and the startup list stays. `Close` stops every server; the SDK closes a stdio server's standard input and waits up to 5 s before stopping it.
<!-- /memoria:section -->

<!-- memoria:section id="calls" files="call.go result.go names.go" -->
## How a call flows

1. The embedded engine's registry offers each tool from `Manager.Tools` under its qualified name: `mcp__<server>__<tool>`, each part cut to `[A-Za-z0-9_]`, at most 64 characters, with 12 hex digits of a SHA-1 when a name is too long or collides.
2. When the model calls one, the engine's translator checks that the arguments are a JSON object, applies the approval mode, and submits a runner remote job (plan `uah.mcp_call`) instead of running the call itself, so the coordinator never waits on a server.
3. The engine's remote job handler calls `Manager.Call` on its own goroutine. A server without `supports_parallel_tool_calls` takes one call at a time; a waiting call is bounded only by cancellation, and `tool_timeout_sec` (default 300 s) starts when the call runs.
4. The SDK sends `tools/call`. A canceled job cancels the call's context, and the SDK tells the server.
5. `convert` turns the result into text and images, as Codex does: structured content replaces the content as JSON text, images become `data:` URLs, and `isError` fails the job with the text. The runner bounds the text (40,000 characters, head and tail) before the model sees it.

A job that was still running when uah stopped fails as interrupted on resume instead of calling the tool twice.
<!-- /memoria:section -->

<!-- memoria:section id="approvals" files="config.go manager.go" -->
## Approvals

Each tool has Codex's `approval_mode`: its own from `[mcp_servers.<name>.tools.<tool>]`, else `default_tools_approval_mode`, else `auto`. `Tool.NeedsApproval` decides: `approve` never asks, `prompt` always asks, `writes` asks unless the tool is annotated read-only, and `auto` asks unless the annotations say read-only, or both non-destructive and closed-world. The engine asks through the session's approval prompt, which PermissionRequest hooks can answer; headless runs and `approval_policy = "never"` refuse with a reason the model reads. `enabled_tools` and `disabled_tools` decide which tools exist at all.
<!-- /memoria:section -->

<!-- memoria:section id="auth" files="auth.go login.go callback.go credentials.go" -->
## Authorization

An HTTP server authorizes with a bearer token (`bearer_token_env_var` or an `Authorization` header) or with OAuth; a stdio server needs neither.

`Login` runs OAuth 2.1 through the SDK's `auth.AuthorizationCodeHandler`: it connects, and the server's 401 starts discovery (protected resource and authorization server metadata), client registration (`oauth.client_id`, else dynamic registration), PKCE, and the token exchange. This package adds the loopback callback on 127.0.0.1 (`/callback`, a port the OS picks unless `oauth.callback_port` or `mcp_oauth_callback_port` sets one), the printed URL, the browser, a 300 s wait, `scopes` and `oauth_resource`, and saving the tokens, including any refresh the SDK does at once.

A running server's `storedAuth` handler sends the stored token and lets the oauth2 package refresh it; each new token is saved back. A 401 never opens a browser: the server becomes `needs_login` with "Run `uah mcp login <name>`", which `/mcp`, `uah doctor`, and `uah mcp list` show.

Logins are stored as Codex stores them, under the server's name and a hash of its URL: in the OS keyring (service "uah MCP Credentials", through `github.com/zalando/go-keyring`), or in `<config dir>/mcp-credentials.json` (0600) when `mcp_oauth_credentials_store` is `file`, or `auto` (the default) and the keyring fails. `AuthStatusOf` reports "Bearer token", "OAuth", "Not logged in", or "Unsupported" without starting the server, with at most 5 s of discovery.
<!-- /memoria:section -->

<!-- memoria:section id="configuration" files="config.go configfile.go" -->
## Configuration and Codex

`ServerConfig` has Codex's keys and meanings, so a Codex `[mcp_servers]` section copies over; unsupported Codex keys are errors rather than ignored. The keys, defaults, and merge rules are in [the configuration reference](../../docs/configuration.md#mcp-servers). Differences from Codex: tool names are at most 64 characters (flat function names instead of namespaces), `auth` accepts only `oauth`, and client ID metadata documents are not offered.

`AddServer` and `RemoveServer` edit a configuration file for `uah mcp add` and `remove` without rewriting it: the go-toml parser finds the server's `[mcp_servers.<name>]` tables, those bytes are cut, and a new table is appended. Comments and the other keys stay as they were. The result must parse and validate before it replaces the file atomically with the same permissions; a server written as an inline table is refused.
<!-- /memoria:section -->

<!-- memoria:section id="extending" files="manager.go server.go" -->
## Extending

- **Resources and prompts.** `server.session` is the SDK's `ClientSession`, which already lists and reads them. Add a lister beside `Tools`, a status field, and, for model access, a remote job plan type beside `uah.mcp_call` in the embedded engine.
- **A new transport.** `Manager.transport` returns the SDK transport for a configuration; add the case and its keys in `ServerConfig.validateTransport`.
- **Another credential store.** Implement `CredentialStore` and add a mode to `NewCredentialStore`.
- **Restarts.** `watch` is where a stopped server is noticed; a restart policy would reconnect there, as `reconnect` does for expired HTTP sessions.
<!-- /memoria:section -->
