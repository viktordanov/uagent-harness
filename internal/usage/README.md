<!-- memoria:section id="overview" files="usage.go payload.go fetch.go reader.go" -->
# Subscription usage

The usage package reads the rate limits of the ChatGPT subscription behind the openai-codex provider: each window (a week, or 5 hours and a week), the percent used, and when it resets. `uah usage`, the TUI's `/status`, footer, and warnings, and `uah doctor` show it; the [usage design record](../../docs/design/usage.md) has the research and the decisions.

<!-- memoria:export id="summary" -->
The usage package reads the ChatGPT subscription's rate limits for the openai-codex provider, as Codex does: one read-only GET to the ChatGPT backend's usage endpoint with the login's credentials and uah's own identity. A reader caches the answer for 60 seconds and sends one request at a time; it reads on demand and after each run, never on a timer. Other providers have no usage.
<!-- /memoria:export -->

1. [The reader](#the-reader)
2. [Reading usage](#reading-usage)
3. [Rows, warnings, and the limit](#rows-warnings-and-the-limit)
4. [Headers and errors](#headers-and-errors)
5. [Tests and the probe](#tests-and-the-probe)

The facts about Codex were checked against Codex `rust-v0.156.1`, and the facts about the runner against unreal-agent v0.1.1.
<!-- /memoria:section -->

<!-- memoria:section id="reader" files="reader.go" -->
## The reader

`Reader.Usage(ctx, maxAge)` returns the latest snapshot, reading the backend when the cached one is older than `maxAge`. `For(provider, opts)` returns the reader for a provider: a `CodexReader` for openai-codex, and for any other provider one whose error wraps `ErrUnsupported` and names it ("usage is not available for ollama").

| Property | How |
| --- | --- |
| One request at a time | A read holds a one-slot semaphore; a caller that waited gets the snapshot of the read that finished meanwhile, even with max age 0. Waiting ends with the context |
| Cache | The last snapshot stays in memory. The TUI's read after a run uses `CacheFor` (60 s); `uah usage`, `/status`, `uah doctor`, and the limit notice use 0, which always reads |
| Credentials | Loaded on each read with `openaicodex.EnvironmentConfig` and `codexauth.Load`, as the engine does, because Codex refreshes its login file |
| Errors | The last snapshot comes back with the error, so `/status` can show it as stale |

Only `internal/app` builds a reader: `app.NewUsage` for the settings' provider and base URL, once per session in `app.Setup` (`Result.Usage`), next to the model catalog. `cmd/uah` passes it to the TUI in `bubble.Deps.Usage`, and `app.Doctor` and `app.ReadUsage` build their own. The TUI's state and renderer use the types and the formatting in this package, never `Fetch`.
<!-- /memoria:section -->

<!-- memoria:section id="usage" files="usage.go payload.go fetch.go" -->
## Reading usage

`Fetch(ctx, creds, opts)` sends one GET to `https://chatgpt.com/backend-api/wham/usage`, the endpoint that Codex's `/status` reads. The credentials are `codexauth.Creds`. The request sends the same headers that the engine sends to `/responses`: the bearer token, `ChatGPT-Account-ID`, and `originator` and `User-Agent` set to `unreal-agent`, not Codex's user agent. The client does not follow redirects, and an error never contains the token or the response body.

`URL` chooses the path as Codex does: `/wham/usage` under `/backend-api`, else `/api/codex/usage`. It accepts only the runner's base URL or a loopback test server, so the token cannot go to another host. `--base-url` (`UNREAL_HARNESS_LLM_BASE_URL`) sets it, as for the model list.

`Parse` turns the body into a `Snapshot`:

| Field | Meaning |
| --- | --- |
| `Plan` | The ChatGPT plan, such as `plus` or `pro` |
| `Limits` | The ordinary limit (`codex`) first, then any per-model limits |
| `Limit.Primary`, `Limit.Secondary` | The two windows; either one can be missing |
| `Window.UsedPercent`, `Minutes`, `ResetsAt` | Percent used, window length, reset time |
| `Credits` | Extra-usage credits, when the backend sends them |
| `ReachedType` | Why a limit is reached, when one is |

A window's name comes from its length, not its position: `Label` returns `5h`, `daily`, `weekly`, `monthly`, or `annual` as Codex does. This is necessary because a Pro login returned only a weekly window, and the backend sent it as the primary window. `ResetLabel` gives Codex's reset time, `15:44` today or `15:44 on 26 Sep`. `Stale` applies Codex's 15-minute limit.
<!-- /memoria:section -->

<!-- memoria:section id="rows" files="rows.go limit.go" -->
## Rows, warnings, and the limit

`Snapshot.Rows` lists every window as a `Row` with its label (an additional limit's name first), and every place that shows usage draws from it:

| Helper | Gives | Used by |
| --- | --- | --- |
| `Row.Left`, `Row.Text` | `78% left`, `weekly 78% left (resets 15:44 on 26 Sep)` | The footer, `uah doctor` |
| `Bar` | A 20-cell bar of the percent left, as Codex's status card | `uah usage`, `/status` |
| `Snapshot.Tightest` | The ordinary limit's window with the least left | The footer, `uah doctor` |
| `Window.Crossed` | The highest of `WarnAt` (75, 90, 95% used) the window reached | The TUI's warnings |
| `Snapshot.Reached`, `BlockedUntil` | Whether a limit is used up, and when the usage frees up | `uah usage`, `uah doctor`, the limit notice |

`LimitReachedIn` reads a run's failure text. The runner reports a failed model call as text only, such as `responses API request failed with status 429: …` or `responses API error usage_limit_reached: …`, and drops the body's `resets_at`. The function accepts `usage_limit_reached`, or 429 with "usage limit", and takes the reset time from the text when the body survived in it; otherwise the TUI reads the usage to say when to try again.
<!-- /memoria:section -->

<!-- memoria:section id="calls" files="headers.go transport.go" -->
## Headers and errors

The backend also sends the rate limits as headers on each `/responses` call (`x-codex-primary-used-percent`, `-window-minutes`, `-reset-at`, the same for `secondary`, and `x-codex-credits-*`). `ParseHeaders` reads them, including extra limit families such as `x-codex-other-primary-used-percent`. `Transport` is an `http.RoundTripper` that calls `Observe` with each response's snapshot. Nothing uses it yet: it can be used only by a client whose `*http.Client` uah builds, because the runner's openai-codex client builds its own client (option B in the design).

`ParseLimitReached` reads the body of a 429 whose error type is `usage_limit_reached`, which gives the plan and the reset time.
<!-- /memoria:section -->

<!-- memoria:section id="development" files="parse_test.go headers_test.go fetch_test.go reader_test.go rows_test.go probe_test.go testdata/pro_weekly_only.json testdata/plus_two_windows.json" -->
## Tests and the probe

`testdata/pro_weekly_only.json` is the response that a Pro login received on 2026-09-24, with the identifiers replaced. `plus_two_windows.json` is synthetic and has two windows, an additional limit, and a reached limit. The header tests use the header names that a real `/responses` call returned. `Fetch` is tested against a loopback server, for the headers, the path, and errors that contain no secrets. The reader is tested against a loopback server with a test clock: the cache, one request for many callers, the context, errors that keep the last snapshot, and unsupported providers. `uah usage`, `uah doctor`, and the TUI are tested against loopback servers too; no test calls the real backend.

`probe_test.go` needs the `probe` build tag and a Codex login. It sends one real GET to the usage endpoint and makes one small model call through `Transport`:

```sh
go test -tags probe -run TestProbe -v ./internal/usage/
```
<!-- /memoria:section -->
