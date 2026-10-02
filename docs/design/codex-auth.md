# Keeping the ChatGPT login fresh

Status: built (ledger item 50). The package READMEs hold the current contract; this record keeps the research and the decisions.

GitHub #1: a uah session left open reported the Codex login as expired, while Codex itself never does. uah read the access token from `$CODEX_HOME/auth.json` and never refreshed it; uagent's preflight blocked a run with an expired token, and the embedded engine sent the token it read when it built the client. Codex refreshes the token with the refresh token in the same file and writes the file back. This record describes what Codex does and how uah does the same.

Contents:

1. [What Codex does](#what-codex-does)
2. [What uah does](#what-uah-does)
3. [Concurrent writers](#concurrent-writers)
4. [The two engines](#the-two-engines)
5. [Failures and `uah doctor`](#failures-and-uah-doctor)
6. [What is left](#what-is-left)

## What Codex does

Checked against Codex rust-v0.156.1, `codex-rs/login/src`.

| Behavior | Codex | Source |
| --- | --- | --- |
| When, before a request | `AuthManager::auth` refreshes a ChatGPT login when the access token's `exp` is within 5 minutes; for a token without an expiry, when `last_refresh` is older than 8 days. A refresh that fails is logged, and the old token is used | `auth/manager.rs:203-204`, `2359-2373`, `2955-2977` |
| When, after a 401 | `UnauthorizedRecovery`: first reload the file (only if the account ID still matches); if the file changed, retry with it; else refresh from the token endpoint; a second 401 goes to the user | `auth/manager.rs:1830-2003` |
| The request | `POST https://auth.openai.com/oauth/token`, `Content-Type: application/json`, body `{"client_id":"app_EMoamEEZ73f0CkXaXp7hrann","grant_type":"refresh_token","refresh_token":"…"}` (no scope). `CODEX_REFRESH_TOKEN_URL_OVERRIDE` replaces the URL | `auth/manager.rs:212-214`, `1611-1658`, `1698`; `oauth/client.rs:76-126` |
| The answer | `id_token`, `access_token`, `refresh_token`, each optional | `auth/manager.rs:1691-1695` |
| What it writes | `persist_tokens` loads the file again, sets each returned token in `tokens` (`id_token` is stored as the raw JWT), sets `last_refresh` to now, and saves | `auth/manager.rs:1584-1607`; `token_data.rs:11-25`, `200-213` |
| How it writes | `serde_json::to_string_pretty` (two-space indent), `open(truncate, write, create)` with mode 0600, in place. No temporary file, no lock. Fields `AuthDotJson` does not know are dropped | `auth/storage.rs:39-65`, `206-222` |
| Concurrency | One refresh at a time in the process (`refresh_lock`, a one-permit semaphore). `refresh_token` reloads the file first: if the token there differs from the cached one, another process refreshed it, and Codex uses that instead of refreshing | `auth/manager.rs:2037`, `2799-2833`, `2445-2478` |
| Refusals | Permanent when the endpoint answers 401, 400 with `invalid_grant`, or the codes `refresh_token_expired`, `refresh_token_reused`, `refresh_token_invalidated` (from `error`, `error.code`, or `code`). The message says to log out and sign in again. A permanent failure is cached for that login, so it is not retried; anything else is transient | `auth/manager.rs:206-211`, `1636-1690`, `2517-2531`; `oauth/error.rs:135-166` |

## What uah does

`internal/engine/codexauth` mirrors this for Codex's auth file. `codexauth.Open(getenv)` returns the `Login` the environment selects, as the runner's client finds it (`OPENAI_CODEX_ACCESS_TOKEN`, else `OPENAI_CODEX_AUTH_FILE`, else `$CODEX_HOME/auth.json`, else `~/.codex/auth.json`).

- **The file is read on each request.** `Login.Creds` stats the file and parses it again only when its size, modification time, or inode changed, so a refresh Codex writes is picked up at once.
- **Before a request**, `Creds` refreshes as Codex does: the token expires within 5 minutes, or has no expiry and `last_refresh` is older than 8 days. If the refresh fails and the token still works, it is used.
- **After a 401**, `Login.Transport` (an `http.RoundTripper`) calls `Login.Renew` with the rejected credentials and sends the request once more. `Renew` uses the file's token when another writer already replaced the rejected one, and refreshes otherwise.
- **The request and the classification** are Codex's, above (`refresh.go`). The token endpoint override is honored only for a loopback address, so tests use a fake server and a workspace `.env` cannot send the refresh token elsewhere.
- **The write** keeps Codex's format: the same members in the same order, the new tokens and `last_refresh` set in place, two-space indentation. Unlike Codex, it also keeps fields it does not know, and it writes a temporary file (mode 0600) beside the file and renames it over the file, following a symlink to its target.
- **One login per file per process.** Every `Login` for a path shares one `file` value, so the engines' clients, subagents, the usage reader, and the model list refresh it once at a time.
- **`OPENAI_CODEX_ACCESS_TOKEN` is never refreshed.** Its behavior is unchanged: an expired token is an error, and a 401 is passed to the caller.

The code is `login.go` (when to refresh, and the refresh under the locks), `refresh.go` (the request, adapted from Codex), `file.go` (reading, the lock, and the write), and `transport.go` (the round tripper and `Status`).

## Concurrent writers

A refresh token works once, so two refreshes with the same token make the second fail as `refresh_token_reused`. `renew` avoids that in three steps:

1. **In the process**, a mutex per file: concurrent callers wait, and each then finds the file already refreshed.
2. **Among uah processes**, an exclusive `flock` on `.auth.json.uah-lock` beside the file. Codex takes no lock, so this orders only uah processes.
3. **Against Codex**, the file is read again under the lock before the request: when the token no longer needs a refresh, another writer refreshed it, and its tokens are used, as Codex's guarded reload does. The file is read once more before the write: if it changed during the request and holds a good token, that writer wins, and uah's new tokens are dropped rather than written over Codex's. When the endpoint refuses the refresh, the file is read again too, since Codex may have used the refresh token first.

The refresh request and the write run without the caller's cancellation (with a 30-second timeout), because a refresh the endpoint answered but uah did not write would lose the only valid refresh token.

## The two engines

- **Embedded.** `clients.go` no longer bakes the token into the Responses client's headers. The HTTP client's transport is the login's, under the watching transport, so each attempt carries the current token and a 401 is renewed and retried inside one attempt. The model list (`internal/models`) uses the same transport; the usage reader renews after a 401 and reads again.
- **Before each run**, on either engine, `codexauth.BeforeRun` refreshes a token that expires within an hour. uagent v0.4.4's preflight warns within an hour and blocks an expired token ("run `codex login`", `harness/preflight.go:18` and `145-157`), so an expired token that can be refreshed never blocks a run, and uagent stays unchanged.
- **Process.** The runner reads the auth file once, when its client is built (`openaicodex/credentials.go`, v0.1.1), and inherits uah's environment, so it reads the same file. Refreshing the file before each run is enough for a run shorter than the token's life. A run that outlives the token fails with the runner's own "credentials rejected" error; the next run starts with a fresh token.

## Failures and `uah doctor`

A refresh the endpoint refuses for good is `codexauth.ErrLoginExpired`: "Your ChatGPT login expired; run `codex login`". The refresh token it refused is remembered, so it is not sent again until the file changes. On the embedded engine, the round tripper then answers the model request with the backend's 401, which the runner's client does not retry, and the model error says the same sentence. A refresh that fails otherwise (a network error, a 5xx) is a plain error, which the runner's client retries with its backoff. No error contains a token or the endpoint's answer.

`uah doctor` reads the login without refreshing it (`Login.Status`, `Login.Check`): the file, when the access token expires or expired, when it was last refreshed, and whether uah can refresh it. An expired or expiring token that uah can refresh is not a failure or a warning, since each run refreshes it first. The usage check reads the usage as the TUI does, which refreshes a login that is due.

## What is left

- Codex can keep its login in the OS keyring (`cli_auth_credentials_store`); the runner and uah read only the file.
- A process-engine run longer than the token's life still fails at the end of the token; the runner would have to read the token per request.
