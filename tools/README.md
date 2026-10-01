# Testing harnesses

<!-- memoria:export id="summary" -->
The harnesses that measure uah rather than test it: the performance harness (`go run ./tools/perf`), the agent benchmark against Codex (`go run ./tools/agentbench`), the compaction evaluation, and the TUI framework benchmark, with what each measures, how to run it, and where its results go.
<!-- /memoria:export -->

A test answers yes or no. A harness answers how much: how long a session takes to load, what a turn costs, how much a compaction frees.
Each harness runs without a network or a real model unless it says so (the agent benchmark does), and each keeps a small bound in `go test` so CI notices a large regression.
New harnesses go in this folder, one folder each, with a README and an entry below.

| Harness | Measures | Run | Results |
| --- | --- | --- | --- |
| [Performance](perf/README.md) | Loading a session (time to the first model request, the TUI's first frame, scrolling), an active turn, subagents and forks, the idle TUI, and leaks, on synthetic sessions of 100 to 10,000 records or copies of real ones: wall and CPU time, allocations, the heap, goroutines, connections, and disk writes | `go run ./tools/perf` | A table, and JSON in `tools/perf/results/` (ignored by git); the committed baseline is `tools/perf/baseline.json` |
| [Agent benchmark](agentbench/README.md) | 25 coding tasks run by `uah exec` and `codex exec` on the same model and effort: pass rate, wall time, and each run's timeline (model, tools, their overlap, idle time, concurrency, tokens, estimated cost). Real model calls; `-dry` validates the tasks without them | `go run ./tools/agentbench` | JSON lines and a markdown report in `tools/agentbench/results/` (ignored by git) |
| [Compaction evaluation](../internal/compaction/README.md#measuring-compaction) | Each compaction strategy on recorded sessions: tokens freed, facts kept, cache damage, re-fetch risk | `uah compaction eval [session file or directory]` (hidden) | Tables of numbers on standard output |
| [TUI framework benchmark](../bench/tui/README.md) | Bubble Tea v2 against Ultraviolet, vaxis, tcell, tview, and gocui: cold start, idle cost, streaming CPU, bytes written, memory | `./bench.sh` in `bench/tui` (a separate Go module) | Tables in its README |

The compaction evaluation stays in `internal/compaction/eval` and `evalrun` because the hidden `uah compaction eval` command imports them, and it runs on the user's own sessions. The TUI benchmark could move to `tools/tui` as it is: it is a separate module that nothing imports. Moving it needs only its links, the `ignore` entry for its `go.sum` in `memoria.toml`, and the paths in its README.
