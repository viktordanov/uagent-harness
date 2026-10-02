# CI triage

The nightly workflow (`nightly/test`) builds the service and runs `gotestsum --rerun-fails=2 -- -race -timeout 10m ./...` against Postgres, Redis, and the mock gateway. A failed test is rerun up to two more times; a test that passes on a rerun does not fail the job.

When the main branch goes red, the on-call engineer triages every failed job into `ci/triage.csv`:

| Column | Holds |
| --- | --- |
| `job` | the job number, as in the log's file name (`job-4119.log` is 4119) |
| `cause` | one of the causes below |
| `test` | the failing top-level test (`TestInvoiceTotals`, not a subtest) for `regression`, `flaky-network`, and `deadlock-timeout`; empty otherwise |
| `commit` | for `regression`, the short hash of the change that most likely broke the test, from the log's "Changes since the last green run"; empty otherwise |

Causes:

| Cause | Means |
| --- | --- |
| `regression` | a test fails on every attempt because the code returns a wrong result |
| `flaky-network` | a test fails on every attempt because a network dependency (Postgres, Redis, the gateway, a webhook target) could not be reached; not a code bug |
| `oom-killed` | the kernel killed a test binary for using too much memory |
| `disk-full` | the build ran out of disk |
| `deadlock-timeout` | the 10-minute test timeout fired while a test hung |
| `infra` | the runner or the service images failed: the job died outside the code |

Each job failed for exactly one cause. Warnings in later steps (artifact upload, cleanup) and flakes that passed on a rerun are not the cause.

Then write `ci/TRIAGE.md` for the team: the number of jobs per cause, and for each distinct regression, the test, its package, the commit that broke it, and the jobs it failed.
