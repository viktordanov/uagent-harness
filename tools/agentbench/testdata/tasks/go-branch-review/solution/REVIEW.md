# Review of feature/quotas against main

## 1. Off-by-one: a tenant gets limit + 1 requests

`internal/quota/quota.go`, line 19, in `Quota.Allow`: the check is
`if used > q.limit`, so when `used == limit` the request is still allowed and
counted. A quota of 10 allows 11 requests per window.

Fix: `if used >= q.limit { return false }`.

## 2. Data race: Increment does not take the lock

`internal/quota/store.go`, line 27, `CounterStore.Increment` writes
`s.counts[tenant]++` without `s.mu`, while `Get` and `Reset` lock. Handlers
call it concurrently, so this is a data race on the map (and can crash with
"concurrent map writes"). Also, `Allow` does `Get` then `Increment` as two
separate steps, so two concurrent requests can both pass at the limit.

Fix: lock in `Increment`, and make the check-and-increment atomic (one
locked method such as `IncrementIfBelow(tenant, limit) bool`).

## 3. Swallowed error: usage that fails to record is billed as success

`internal/server/handler.go`, line 68, `recordUsage` returns `nil` when
`h.usage.Record` fails, so the error is swallowed: the handler answers 202 and
the usage is never billed. On main the handler returned 503.

Fix: `return err` (or wrap it), so `ServeHTTP` answers 503 as before.
