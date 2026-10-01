<!-- memoria:section id="overview" files="main.go perf/env.go" -->
# Performance harness

<!-- memoria:export id="summary" -->
`go run ./tools/perf` measures what uah costs outside the model: loading a session, the TUI's first frame and scrolling, an active turn, subagents and forks, the idle TUI, and leaks, on synthetic sessions of 100 to 10,000 records or on copies of real ones, against a scripted fake model. It prints a table, saves JSON, and compares a run with a baseline.
<!-- /memoria:export -->

The harness drives uah's real stack: `app.Setup` and `session.Open` as `uah` resumes a session, the embedded engine with its sandbox, and the TUI in a real Bubble Tea program. The model is [`testing/fakellm`](../../testing/fakellm/fakellm.go), so no run needs a network, a login, or tokens. Every scenario runs in its own scratch home, and the process environment points `HOME`, `UAH_HOME`, `CODEX_HOME`, and the XDG directories there, so the harness never reads `~/.uah`, `~/.codex`, or the user's configuration.

1. [Run it](#run-it)
2. [Scenarios](#scenarios)
3. [What a sample measures](#what-a-sample-measures)
4. [How fixtures are built](#how-fixtures-are-built)
5. [Real sessions](#real-sessions)
6. [Baseline](#baseline)
7. [The test](#the-test)

It is a command under `tools/` rather than a hidden `uah` subcommand, as `uah compaction eval` is, because it needs nothing of the user's: the fake model and the fixture builder are test code, and `go run` keeps them out of the shipped binary.
<!-- /memoria:section -->

<!-- memoria:section id="usage" files="main.go perf/run.go perf/report.go perf/compare.go" -->
## Run it

Run every command from the repository root.

```sh
go run ./tools/perf                         # every scenario, small, medium, and large: about 2 minutes on main
go run ./tools/perf -sizes small -run 'load|tui'   # a subset (the regular expression matches scenario names)
go run ./tools/perf -count 3                # each scenario three times; the report keeps the medians
go run ./tools/perf -baseline tools/perf/baseline.json   # run, then compare; exit status 3 on a regression
go run ./tools/perf -compare old.json new.json           # compare two saved reports without running
go run ./tools/perf -run 'turn/large' -cpuprofile -memprofile   # profiles of each scenario that runs
go run ./tools/perf -run leak -goroutines   # the stacks of the goroutines each scenario leaves behind
```

Each run prints a table and a line of each scenario's own measurements, and saves the report as JSON in `tools/perf/results/<time>.json` (`-out` names another file). `results/` is ignored by git. Profiles go next to the report in `<report>-profiles/` (`-profiles` names another folder): `<scenario>.cpu.pprof`, and `<scenario>.mem-before.pprof` and `<scenario>.mem.pprof`, whose difference is the scenario's allocations (`go tool pprof -base <scenario>.mem-before.pprof <scenario>.mem.pprof`).

A comparison lists each metric that moved by more than `-threshold` (default 0.25, that is 25%) and by more than the metric's floor, as `REGRESSION` or `better`; `-all` lists every metric. The floors keep noise on small numbers out: 5 ms for times, 1 MB for sizes, 3 goroutines, 1 connection, 20,000 allocations, 200 wakeups. Metrics that depend on the scenarios before (`goroutines_before`, `goroutines_after`) or describe the run (`events`, `views`, `request_mb`, `server_ms`) are never compared.

Numbers vary with the machine and its load. Compare runs from one machine, with `-count 3` when the change is small.
<!-- /memoria:section -->

<!-- memoria:section id="scenarios" files="perf/scenarios.go perf/tui.go perf/workload.go" -->
## Scenarios

Each scenario builds its session in a fresh scratch home, measures one block, and cleans up. Scenarios marked "per size" run on each size of `-sizes` and on each copied real session; the others run on the small fixture.

| Scenario | Per size | Block | Its own measurements |
| --- | --- | --- | --- |
| `load` | yes | Resume the session as `uah resume` does (index already built) and send one message, to the first request and the end of the run | `open_ms`, `first_request_ms` (message to the request's arrival at the fake model), `history_ms` (`session.Load`, the TUI's transcript, outside the block), `index_ms` (building the SQLite index, outside the block) |
| `tui` | yes | Start the TUI on the session; wait for the first frame of the resumed transcript; page up 20 times, then down 20 times | `first_frame_ms` (the view after the TUI takes the opened session), `first_paint_ms` (the renderer's first write after it), `scroll_p50_ms`, `scroll_p95_ms`, `scroll_max_ms` (a key to its view), `view_*_ms` (the model's View), `term_kb` |
| `turn` | yes | Resume the session and run the workload turn headless | `turn_ms`, `first_request_ms`, `events`, `events_per_s`, `records_appended` (records the turn added to the session file), `requests`, `request_mb` |
| `spawn` | yes | Resume the session; the model spawns one child, waits, and finishes | `child_first_request_ms` (the parent's reply to the child's first request, once the fake model has read it; see `server_ms`) |
| `fork` | yes | The same with `fork_context`: the child copies the whole history | `child_first_request_ms` |
| `tui-turn` | no | The workload turn typed into the TUI, until the answer shows and the footer is idle | `turn_ms`, `views`, `view_*_ms`, `term_kb`, `term_writes` |
| `idle/tui` | no | The same TUI for 3 seconds after that turn | `updates_per_s` (0: the TUI's clock stopped), `views_per_s`, `cpu_ms_per_s`, `wakeups_per_s`, `term_bytes_per_s` |
| `agents` | no | One turn that spawns two children and forks one, waits for all, and finishes | `spawn_a_ms`, `spawn_b_ms`, `fork_ms`, `peak_goroutines`, `peak_conns` |
| `leak` | no | Open the session, run a turn with one command, close it; five times | `goroutines_left`, `conns_after` |

The workload turn ([workload.go](perf/workload.go)) is what one user turn of a coding session sends uah: four model responses with reasoning summaries and streamed commentary, six shell commands that read Go-like source (4 to 24 kB) and a 64 kB test log, one `apply_patch`, and a final answer in Markdown. The fake model answers at once, so a turn's time is uah's: the engine, the sandboxed commands, the session file, and the run record.

The TUI runs at 120×40 in true color. Its input never comes; the harness sends keys as messages, and a probe around the model counts each Update and times each View. The terminal counts the bytes and writes the renderer sends and drops them.
<!-- /memoria:section -->

<!-- memoria:section id="metrics" files="perf/metrics.go perf/sys_darwin.go perf/sys_linux.go perf/sys_other.go perf/report.go" -->
## What a sample measures

Every scenario reports the same columns for its block:

| Metric | Meaning |
| --- | --- |
| `wall_ms` | Wall time of the block |
| `cpu_ms`, `child_cpu_ms` | The process's user and system time (`getrusage`), and the commands' it waited for |
| `alloc_mb`, `allocs` | What the Go heap allocated (`runtime/metrics`) |
| `peak_heap_mb` | The most live heap, sampled every 2 ms |
| `goroutines_before`, `goroutines_after`, `goroutines_left` | Goroutines before the block and 0.5 s after its cleanup; `left` is the difference less one goroutine per open connection, which is the fake model's server, not uah's |
| `conns_after` | Connections to the fake model still open after the cleanup, idle ones included |
| `disk_written_mb` | Bytes the process wrote to disk: the kernel's count on macOS (`proc_pid_rusage`), `write_bytes` of `/proc/self/io` on Linux |
| `state_growth_mb` | How much the scratch home grew |
| `wakeups` | macOS: idle and interrupt wakeups; Linux: voluntary context switches |

The number of `fsync` calls is not measurable from inside the process. `records_appended` stands in for it: the session store writes and syncs each record on its own.

The fake model runs in the same process. Scenarios without subagents set `fakellm.Server.Light`, so it reads each request's bytes and parses nothing; the subagent scenarios need the parsed requests to route children, and `server_ms` reports the fake model's own time.
<!-- /memoria:section -->

<!-- memoria:section id="fixtures" files="perf/fixture.go perf/workload.go" -->
## How fixtures are built

A session of 10,000 records cannot be made by running turns: every model request carries the whole history, so it would take minutes. The harness records two real turns once per run, a seed and the workload turn, on the real stack, and a fixture repeats the workload turn's records:

1. The session file's items and operation records, split into tokens: text kept as is, and the values each copy changes.
2. Each copy gets fresh IDs (each UUID's fourth group is the copy's number; the session ID stays), fakellm's response and call IDs with the copy's number, sequence numbers moved past the previous copy, times 2 minutes apart ending now, and the scratch home's path.
3. The first turn of a copy follows the previous copy's last turn, so the turn chain is whole.
4. Each copy gets its run record (`events.jsonl`, `request.json`, `summary.json`, `stderr.log`) and the operations' output files.

| Size | Records | Session file | Runs |
| --- | --- | --- | --- |
| small | 80 | 0.4 MB | 2 |
| medium | 2,030 | 11.4 MB | 28 |
| large | 9,980 | 56 MB | 134 |

One workload turn writes 75 records: shell output is recorded as the command runs, several times per command. What uah reads back is what it wrote; only the IDs, times, and paths differ.
<!-- /memoria:section -->

<!-- memoria:section id="real" files="perf/real.go" -->
## Real sessions

`-real <uah home>` adds the largest sessions of that home (`-real-sessions`, default 3) to the per-size scenarios `load`, `tui`, and `turn`, as `real-1`, `real-2`, and so on. Each is copied into the scratch home: its session file, sidecar, operation outputs, and run records, with the home's path rewritten. The harness never writes to that home and reads nothing else from it: no configuration, credentials, or index.

A session with an operation that never finished is skipped: resuming it would carry the operation on, and its recorded paths can point outside the copy. The turn on a real session answers with text only, since its workspace is not here. A recorded `SkillUse` call needs its tool to restore, so the scratch workspace has one stub skill.
<!-- /memoria:section -->

<!-- memoria:section id="baseline" files="baseline.json" -->
## Baseline

[baseline.json](baseline.json) is the report of `go run ./tools/perf -count 3` on main at 1eaf678 (v1.5.3), the medians of three runs on an Apple M4 Max (14 cores), macOS 27.2, Go 1.27.1, in the workspace-write sandbox. Compare a change with it on a similar machine: `go run ./tools/perf -baseline tools/perf/baseline.json`. Replace it, with a new commit and this paragraph, when a change moves the numbers on purpose.

| Scenario | Wall ms | CPU ms | Alloc MB | Peak heap MB | Goroutines left | Conns after | Its own |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `load/small` | 84.1 | 28.9 | 13.8 | 11.1 | 2 | 1 | first_request_ms 59.2 |
| `load/medium` | 362 | 374 | 378 | 66.5 | 2 | 1 | first_request_ms 311 |
| `load/large` | 3,414 | 3,651 | 4,790 | 284 | 2 | 1 | first_request_ms 3,308 |
| `tui/small` | 25.1 | 19.0 | 10.6 | 12.0 | 0 | 0 | first_frame_ms 10.8, scroll_p95_ms 0.36 |
| `tui/medium` | 61.0 | 81.0 | 88.5 | 17.1 | 0 | 0 | first_frame_ms 38.5, scroll_p95_ms 0.53 |
| `tui/large` | 212 | 367 | 403 | 20.5 | 0 | 0 | first_frame_ms 196, scroll_p95_ms 0.43 |
| `turn/small` | 689 | 85.9 | 34.0 | 12.5 | 2 | 1 | turn_ms 689, records_appended 75 |
| `turn/medium` | 923 | 440 | 624 | 67.1 | 2 | 1 | turn_ms 921, records_appended 75 |
| `turn/large` | 4,062 | 3,895 | 5,948 | 294 | 2 | 1 | turn_ms 4,050, records_appended 75 |
| `spawn/small` | 182 | 44.6 | 23.9 | 13.6 | 3 | 2 | child_first_request_ms 85.9 |
| `spawn/medium` | 462 | 406 | 565 | 68.9 | 3 | 2 | child_first_request_ms 72.0 |
| `spawn/large` | 3,877 | 4,091 | 5,694 | 295 | 3 | 2 | child_first_request_ms 63.9 |
| `fork/small` | 600 | 110 | 33.1 | 14.3 | 3 | 2 | child_first_request_ms 306, disk_written_mb 1.1 |
| `fork/medium` | 10,317 | 2,054 | 866 | 81.1 | 3 | 2 | child_first_request_ms 4,533, disk_written_mb 27.6 |
| `fork/large` | 52,028 | 17,946 | 9,092 | 377 | 3 | 2 | child_first_request_ms 23,293, disk_written_mb 138 |
| `tui-turn/small` | 613 | 169 | 39.3 | 23.8 | 3 | 1 | view_p95_ms 0.2 |
| `idle/tui` | 3,001 | 31.9 | 0.02 | 0 | -1 | 1 | cpu_ms_per_s 10.6, updates_per_s 0, wakeups_per_s 331 |
| `agents/small` | 487 | 198 | 40.7 | 29.0 | 7 | 4 | fork_ms 307 |
| `leak/5-runs` | 812 | 262 | 92.6 | 23.5 | 10 | 5 | goroutines_left 10 |
<!-- /memoria:section -->

<!-- memoria:section id="test" files="perf/perf_test.go perf/race_test.go perf/norace_test.go" -->
## The test

`go test ./tools/perf/...` runs every scenario on the small fixture once, about 13 seconds (20 under `-race`), and checks generous ceilings: about ten times the baseline, five times more under the race detector. It catches a large regression, such as the TUI's clock running while idle, a turn that allocates ten times as much, or goroutines left by every session, without failing on a slow machine. `go test -short` skips it.
<!-- /memoria:section -->
