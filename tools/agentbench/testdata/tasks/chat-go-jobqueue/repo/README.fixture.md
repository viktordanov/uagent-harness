# jobq

A small job queue: a Go library and the `jobq` command line tool on top of
it. Jobs live in a JSON file; `jobq run` works through them with a pool of
workers, retrying the attempts that fail.

```
go build ./cmd/jobq
./jobq add echo "hello"
./jobq add flaky 2
./jobq run
./jobq list
```

## Layout

| Package | What it holds |
| --- | --- |
| `queue` | the job type, its states, the `Store` interface, and `Queue`, which moves jobs between states |
| `store` | `Memory`, an in-memory store, and `File`, which keeps a `Memory` in a JSON file |
| `worker` | the handler registry and `Pool`, the workers that run jobs |
| `retry` | the retry policy: how many attempts, and the wait between them |
| `metrics` | counters of what a pool did, in all and per kind |
| `internal/clock` | the clock every package reads, with a fake for tests |
| `cmd/jobq` | the command line tool |

## Commands

Every command takes `--store FILE`. Without it, jobq uses `$JOBQ_STORE`, or
`jobq.json` in the current directory. Flags come before arguments.

| Command | Does |
| --- | --- |
| `jobq add [--delay D] KIND [PAYLOAD]` | adds a job and prints its ID; with `--delay`, the job waits that long before it can run |
| `jobq list [--state S] [--json]` | lists jobs, oldest first: ID, state, kind, attempts, payload, last error |
| `jobq show ID` | prints one job as JSON |
| `jobq run [flags]` | runs jobs until none is pending or running |
| `jobq requeue ID...` | puts failed jobs back to pending, with their attempts reset |
| `jobq purge --state S [--older-than D]` | deletes finished jobs of one state |

Flags of `jobq run`:

| Flag | Default | Meaning |
| --- | --- | --- |
| `--workers N` | 4 | jobs run at a time |
| `--poll D` | 20ms | how often an idle worker looks again while a job waits for its retry |
| `--timeout D` | none | stop the run after D; the jobs left are picked up by the next run |
| `--job-timeout D` | none | fail an attempt that takes longer than D |
| `--quiet` | off | do not log failed attempts to stderr |
| `--table` | off | print counts per kind after the summary |

`jobq run` prints the output of the jobs and a summary line to stdout, and one
line per failed attempt to stderr. It exits 1 when a job failed for good.

Exit codes: 0 success, 1 an error while running, 2 a usage error.

## Job kinds

| Kind | Payload | Does |
| --- | --- | --- |
| `echo` | any text | prints `ID: PAYLOAD` |
| `fail` | ignored | fails every attempt |
| `flaky` | a count N | fails its first N attempts, then succeeds |
| `sleep` | a duration | waits, then succeeds |

## States

A job is `pending`, `running`, `done` or `failed`. Each attempt that fails is
retried after a delay, until the job has used up its attempts; then it is
`failed`. A job left `running` by a run that was killed is put back to
`pending` when the next `jobq run` starts.

## The store file

```json
{
  "version": 1,
  "next_id": 2,
  "jobs": [
    {"id": "job-0001", "kind": "echo", "payload": "hello", "state": "done", "attempts": 1, ...}
  ]
}
```

The file is rewritten after every change, through a temporary file and a
rename. Only one jobq process should use a file at a time.

## Tests

```
go test ./...
```

The tests of `queue` and `worker` run on the fake clock in `internal/clock`,
so retries and delays take no real time.
