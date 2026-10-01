# INC-2291 notes (Ana Petrova, SRE on call)

- Paged 14:07 for 5xx > 1% on checkout-api (prod-eu only; prod-us fine all along).
- First suspicion was Postgres. It was not: CPU under 20%, plenty of
  connection headroom on the server side, no slow queries in pg_stat_statements.
- The Redis latency warning at 14:19 was a red herring: the nightly
  sessions-cache compaction, same window every day.
- What actually showed it: the `db_pool_wait_seconds` histogram. Requests were
  waiting 4-9 s for a connection from the pool, then hitting the 10 s HTTP
  timeout or the client giving up. Pods were capped at 20 open connections.
- We rolled back instead of hotfixing because a rollback is a known-good
  state and the hotfix would have needed a review.
- Customer side: support logged 37 tickets about failed payments, all
  prod-eu. No double charges found (idempotency keys held).

## Follow-ups I'll own

- Ana Petrova: add a paging alert on `db_pool_wait_seconds` p95 > 500 ms, so
  pool exhaustion pages before the 5xx alert does.
- Ana Petrova: put the pool wait panel on the checkout-api dashboard's first row.

## Things that went well

- Rollback took 6 minutes end to end.
- Idempotency keys meant clients could safely retry.
