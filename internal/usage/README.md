<!-- memoria:section id="overview" files="usage.go payload.go fetch.go" -->
# Subscription usage

The usage package reads the rate limits of the ChatGPT subscription behind the openai-codex provider: each window (5 hours and a week today), the percent used, and when it resets. It is a prototype and nothing uses it yet; the [usage design record](../../docs/design/usage.md) has the plan and the open decisions.

<!-- memoria:export id="summary" -->
The usage package reads the ChatGPT subscription's rate limits for the openai-codex provider, as Codex does: one read-only GET to the ChatGPT backend's usage endpoint with the login's credentials, or the rate-limit headers of a model response. It is a prototype that nothing uses yet.
<!-- /memoria:export -->

1. [Reading usage](#reading-usage)
2. [Headers and errors](#headers-and-errors)
3. [Tests and the probe](#tests-and-the-probe)

The facts about Codex were checked against Codex `rust-v0.156.1`, and the facts about the runner against unreal-agent v0.1.1.
<!-- /memoria:section -->

<!-- memoria:section id="usage" files="usage.go payload.go fetch.go" -->
## Reading usage

`Fetch(ctx, creds, opts)` sends one GET to `https://chatgpt.com/backend-api/wham/usage`, the endpoint that Codex's `/status` reads. The credentials are `codexauth.Creds`, which `codexauth.Load` reads from the Codex login as the engine does. The request sends the same headers that the engine sends to `/responses`: the bearer token, `ChatGPT-Account-ID`, and `originator` and `User-Agent` set to `unreal-agent`. The client does not follow redirects, and an error never contains the token or the response body.

`URL` chooses the path as Codex does: `/wham/usage` under `/backend-api`, else `/api/codex/usage`. It accepts only the runner's base URL or a loopback test server, so the token cannot go to another host.

`Parse` turns the body into a `Snapshot`:

| Field | Meaning |
| --- | --- |
| `Plan` | The ChatGPT plan, such as `plus` or `pro` |
| `Limits` | The ordinary limit (`codex`) first, then any per-model limits |
| `Limit.Primary`, `Limit.Secondary` | The two windows; either one can be missing |
| `Window.UsedPercent`, `Minutes`, `ResetsAt` | Percent used, window length, reset time |
| `Credits` | Extra-usage credits, when the backend sends them |
| `ReachedType` | Why a limit is reached, when one is |

A window's name comes from its length, not its position: `Label` returns `5h`, `daily`, `weekly`, `monthly`, or `annual` as Codex does. This is necessary because a Pro login returned only a weekly window, and the backend sent it as the primary window. `String` gives Codex's status text, such as `weekly 78% left (resets 15:44 on 26 Sep)`. `Stale` applies Codex's 15-minute limit.
<!-- /memoria:section -->

<!-- memoria:section id="calls" files="headers.go transport.go" -->
## Headers and errors

The backend also sends the rate limits as headers on each `/responses` call (`x-codex-primary-used-percent`, `-window-minutes`, `-reset-at`, the same for `secondary`, and `x-codex-credits-*`). `ParseHeaders` reads them, including extra limit families such as `x-codex-other-primary-used-percent`. `Transport` is an `http.RoundTripper` that calls `Observe` with each response's snapshot. It can be used only by a client whose `*http.Client` uah builds, because the runner's openai-codex client builds its own client.

`ParseLimitReached` reads the body of a 429 whose error type is `usage_limit_reached`, which gives the plan and the reset time.
<!-- /memoria:section -->

<!-- memoria:section id="development" files="parse_test.go headers_test.go fetch_test.go probe_test.go testdata/pro_weekly_only.json testdata/plus_two_windows.json" -->
## Tests and the probe

`testdata/pro_weekly_only.json` is the response that a Pro login received on 2026-09-24, with the identifiers replaced. `plus_two_windows.json` is synthetic and has two windows, an additional limit, and a reached limit. The header tests use the header names that a real `/responses` call returned. `Fetch` is tested against a loopback server, for the headers, the path, and errors that contain no secrets.

`probe_test.go` needs the `probe` build tag and a Codex login. It sends one real GET to the usage endpoint and makes one small model call through `Transport`:

```sh
go test -tags probe -run TestProbe -v ./internal/usage/
```
<!-- /memoria:section -->
