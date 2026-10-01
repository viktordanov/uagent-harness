# INC-2291 findings: checkout-api failures in prod-eu, 2026-09-14

Audience: engineering leadership. Sources: the deploy and chat timeline, the
per-minute metrics export, Ana Petrova's and Ravi Shah's notes, the config
diff of PR #4412, the alert log, and the support summary.

## Summary

For 45 minutes on 2026-09-14 (14:06 to 14:50 UTC), checkout-api in prod-eu
failed **1,284** requests with 5xx errors, and its p99 latency peaked at
**8,730 ms** at 14:31 against a normal of about 200 ms. The cause was a
configuration change in release 2026.09.14-1 (PR #4412) that lowered the
database connection pool's maximum size, `max_open_conns`
(`DB_MAX_OPEN_CONNS`), **from 200 to 20** per pod in the prod-eu overlay.
Requests queued for a connection from the pool until they hit the 10 s HTTP
timeout. A rollback to 2026.09.13-2 finished at 14:47 and errors stopped by
14:51. prod-us was not affected. No customer was charged twice.

## Root cause

PR #4412 ("checkout: tune db settings for new region") was meant to raise
`max_idle_conns` and shorten `conn_max_lifetime` for the new region's
traffic and proxy. The author edited the wrong line: in
`deploy/overlays/prod-eu/checkout-api.yaml`, `DB_MAX_OPEN_CONNS` went from
`"200"` to `"20"`, while `DB_MAX_IDLE_CONNS` stayed at `"50"` and
`DB_CONN_MAX_LIFETIME` went from `30m` to `15m` as intended. The result is an
invalid combination (idle 50 > open 20) that nothing rejected: the change
looked like routine tuning in review, config-only changes skip the canary
stage, and no validation compares the two values.

Why 20 connections per pod was not enough: 12 pods × 20 = 240 connections
looks like plenty, but each checkout request holds its connection for the
whole checkout transaction, about 300 to 400 ms under load, partly because
the fraud-score call runs inside the transaction. At about 1,000 requests a
minute with bursts, each pod's pool saturated and requests waited for a
connection. The `db_pool_wait_seconds` histogram showed waits of 4 to 9 s;
with the 10 s `HTTP_TIMEOUT`, a request that waited long enough timed out
and returned 5xx.

The database itself was healthy throughout: Postgres CPU stayed under 20%,
there were no slow queries, and the server had connection headroom. The
bottleneck was entirely the client-side pool.

### A red herring: the Redis latency alert

`RedisLatencyHigh` fired at 14:19 and resolved at 14:24 on sessions-cache.
It was unrelated: it is the nightly sessions-cache compaction, which runs in
the same window every day. It cost some investigation time but did not
contribute to the failures.

## Timeline (UTC)

| Time | Event |
| --- | --- |
| 13:58 | Release 2026.09.14-1 (PR #4412) approved by Ravi Shah. |
| 14:02 | Rollout to prod-eu starts (12 pods). |
| 14:04 | Rollout complete. |
| 14:06 | First failed requests; `CheckoutApi5xxRateHigh` fires (5xx 1.2% > 1%). |
| 14:07 | INC-2291 paged to Ana Petrova (SRE on call). |
| 14:08 | `CheckoutApiP99High` fires. |
| 14:09 | Ana acknowledges; p99 climbing, database looks idle. |
| 14:14 | Postgres ruled out (CPU 18%, connection headroom). |
| 14:19 | `RedisLatencyHigh` fires on sessions-cache. |
| 14:24 | Redis alert resolves; identified as the nightly compaction, unrelated. |
| 14:27 | Ravi Shah joins and checks PR #4412's diff. |
| 14:31 | Pool wait metric shows 4–9 s waits; pods capped at 20 connections. p99 peaks at 8,730 ms. |
| 14:33 | Ravi finds `max_open_conns` changed from 200 to 20 instead of `max_idle_conns`. |
| 14:35 | Decision: roll back to 2026.09.13-2 rather than hotfix. |
| 14:41 | Rollback requested. |
| 14:44 | Rollback rolling out. |
| 14:47 | Rollback complete, 12/12 pods ready. |
| 14:50 | Last minute with failed requests. |
| 14:51 | `CheckoutApi5xxRateHigh` resolves. |
| 14:52 | `CheckoutApiP99High` resolves. |
| 14:58 | Error rate and p99 (~200 ms) back to baseline. |
| 15:29 | Incident resolved after 30 minutes of monitoring. |

Time to detect: 2 minutes after the rollout finished (alert at 14:06). Time
to identify the cause: 27 minutes after the page (14:33). Time to mitigate:
14 minutes from the cause to a completed rollback (14:47). Total customer
impact: 45 minutes.

## Customer impact

Computed from `sources/metrics.csv` (per-minute export, 13:50 to 15:05):

- **Failed requests: 1,284** 5xx responses, all between 14:06 and 14:50 (45
  minutes, every minute of that window had failures).
- **Requests in the impact window: 44,161**, so **2.9%** of
  checkout requests in those 45 minutes failed.
- **Peak p99 latency: 8,730 ms** at 14:31; outside the window p99 stayed
  between 180 and 220 ms.
- **Duration: 45 minutes** of failures, prod-eu only.
- Support received **37 tickets** tagged `payment-failed`, all from EU
  customers. Three asked about double charges; finance confirmed **no
  duplicate captures**, because clients retried with idempotency keys.
  Eleven asked for an apology discount and were referred to the account team.

### Per-minute detail of the impact window

| Minute | Requests | Failed (5xx) | Failed % | p99 ms |
| --- | ---: | ---: | ---: | ---: |
| 14:06 | 1,009 | 7 | 0.7% | 931 |
| 14:07 | 930 | 8 | 0.9% | 1,273 |
| 14:08 | 958 | 12 | 1.3% | 1,567 |
| 14:09 | 983 | 16 | 1.6% | 1,843 |
| 14:10 | 1,013 | 13 | 1.3% | 2,157 |
| 14:11 | 956 | 15 | 1.6% | 2,477 |
| 14:12 | 929 | 22 | 2.4% | 2,715 |
| 14:13 | 985 | 22 | 2.2% | 3,053 |
| 14:14 | 941 | 20 | 2.1% | 3,343 |
| 14:15 | 939 | 24 | 2.6% | 3,662 |
| 14:16 | 973 | 28 | 2.9% | 3,905 |
| 14:17 | 1,005 | 26 | 2.6% | 4,209 |
| 14:18 | 1,017 | 32 | 3.1% | 4,571 |
| 14:19 | 993 | 31 | 3.1% | 4,840 |
| 14:20 | 963 | 32 | 3.3% | 5,188 |
| 14:21 | 964 | 34 | 3.5% | 5,476 |
| 14:22 | 983 | 39 | 4.0% | 5,774 |
| 14:23 | 1,022 | 40 | 3.9% | 6,058 |
| 14:24 | 928 | 40 | 4.3% | 6,311 |
| 14:25 | 1,040 | 42 | 4.0% | 6,634 |
| 14:26 | 980 | 43 | 4.4% | 6,989 |
| 14:27 | 1,005 | 49 | 4.9% | 7,208 |
| 14:28 | 927 | 50 | 5.4% | 7,589 |
| 14:29 | 959 | 49 | 5.1% | 7,882 |
| 14:30 | 993 | 57 | 5.7% | 8,187 |
| 14:31 | 1,025 | 47 | 4.6% | 8,730 |
| 14:32 | 956 | 40 | 4.2% | 6,649 |
| 14:33 | 1,033 | 39 | 3.8% | 6,385 |
| 14:34 | 964 | 41 | 4.3% | 6,002 |
| 14:35 | 1,040 | 39 | 3.8% | 5,759 |
| 14:36 | 965 | 37 | 3.8% | 5,421 |
| 14:37 | 998 | 31 | 3.1% | 5,114 |
| 14:38 | 983 | 33 | 3.4% | 4,807 |
| 14:39 | 947 | 31 | 3.3% | 4,536 |
| 14:40 | 936 | 28 | 3.0% | 4,231 |
| 14:41 | 970 | 23 | 2.4% | 3,950 |
| 14:42 | 1,037 | 22 | 2.1% | 3,663 |
| 14:43 | 930 | 19 | 2.0% | 3,321 |
| 14:44 | 977 | 21 | 2.1% | 3,051 |
| 14:45 | 990 | 21 | 2.1% | 2,735 |
| 14:46 | 1,033 | 15 | 1.5% | 2,417 |
| 14:47 | 1,024 | 14 | 1.4% | 2,155 |
| 14:48 | 1,030 | 13 | 1.3% | 1,870 |
| 14:49 | 955 | 9 | 0.9% | 1,590 |
| 14:50 | 973 | 10 | 1.0% | 1,245 |

## What went well

- Detection was fast: the 5xx alert fired two minutes after the rollout
  completed and paged the right person.
- The rollback took 6 minutes end to end and was a known-good state, so the
  team did not have to review a hotfix under pressure.
- Idempotency keys held: clients could retry safely and nobody was charged
  twice.
- The database was ruled out quickly with data (CPU, query stats) rather
  than assumed.

## What went badly

- The change itself: one wrong line in a config overlay took down checkout
  in a region.
- Review did not catch it. The diff looked like tuning, and an idle limit
  above the open limit is not obviously wrong to a reviewer.
- Config-only changes skip the canary stage, so the change reached all 12
  pods at once instead of one.
- There was no alert on pool wait time, which was the direct symptom; the
  team found the pool metric 24 minutes into the incident.
- The Redis alert cost investigation time because a known daily compaction
  pages as a warning.
- Each checkout holds a database connection for the fraud-score call, which
  makes the service far more sensitive to pool size than it needs to be.

## Action items

| # | Action | Owner |
| --- | --- | --- |
| 1 | Page on `db_pool_wait_seconds` p95 > 500 ms, so pool exhaustion pages before the 5xx alert. | Ana Petrova |
| 2 | Put the pool wait panel on the first row of the checkout-api dashboard. | Ana Petrova |
| 3 | CI validation for deploy overlays: reject `max_idle_conns` > `max_open_conns`, and require a second reviewer for any pool size change above 50%. | Ravi Shah |
| 4 | Move the fraud-score call out of the checkout database transaction, to shorten how long each request holds a connection. | Ravi Shah |
| 5 | Send config-only changes through the canary stage like code changes, so a bad config reaches 1 pod, not 12. | Mei Lin (platform) |

Not an action item but worth recording: silence or downgrade the
sessions-cache latency warning during its known compaction window, so it
stops competing for attention during real incidents.

## Contributing factors in detail

1. **Wrong field edited.** The overlay lists `DB_MAX_OPEN_CONNS` directly
   above `DB_MAX_IDLE_CONNS`, with near-identical comments. The intended
   change to the idle limit landed on the open limit.
2. **No semantic validation.** The deploy pipeline checks that the overlay is
   valid YAML and that the variables exist, but not that their values make
   sense together. `max_idle_conns` greater than `max_open_conns` is never a
   correct setting; the database driver silently lowers the idle limit to
   the open limit.
3. **No canary for configuration.** Code changes go to one canary pod for 15
   minutes before the fleet; configuration-only releases skip that stage.
   With a canary, the pool wait alert (once it exists) or the error rate of a
   single pod would have stopped the rollout at 1 of 12 pods.
4. **Long-held connections.** Because the fraud-score call runs inside the
   checkout transaction, each request holds a connection for 300–400 ms
   under load. Halving that time would have roughly halved the pool pressure
   and might have kept the service up even at 20 connections per pod.
5. **Region-specific overlay.** Only prod-eu had the change. That limited the
   blast radius, but it also meant prod-us looked healthy and briefly
   suggested a regional infrastructure problem rather than a release.

## Open questions

- Should pool sizes live in one shared base config with per-region overrides
  allowed only within a range?
- Is a 10 s `HTTP_TIMEOUT` right for checkout? A shorter timeout with a
  bounded pool wait would fail faster and free capacity sooner, at the cost of
  more failures for slow but successful requests.
- Do other services hold connections across external calls the way checkout
  does with fraud scoring?

## Sources

- `sources/timeline.log`: deploy, alert, paging, and chat events.
- `sources/metrics.csv`: per-minute requests, 5xx count, and p99 latency for
  checkout-api in prod-eu, 13:50–15:05 UTC.
- `sources/notes-ana.md`: the on-call SRE's investigation notes and
  follow-ups.
- `sources/notes-ravi.md`: the PR author's account of the change and action
  items.
- `sources/config.diff`: the overlay change in PR #4412.
- `sources/alerts.txt`: alert firing and resolution times.
- `sources/support-summary.md`: customer tickets and finance's double-charge
  check.
