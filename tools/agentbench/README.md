<!-- memoria:section id="overview" files="main.go bench/run.go bench/harness.go" -->
# Agent benchmark

<!-- memoria:export id="summary" -->
`go run ./tools/agentbench` runs a suite of small coding tasks with `uah exec` and `codex exec` on the same model and effort, checks each result, records each run's timeline (model requests, tool calls, their overlap, tokens), and writes JSON results and a markdown report that compares uah with Codex. It makes real model calls, except with `-dry`.
<!-- /memoria:export -->

The [performance harness](../perf/README.md) measures what uah costs around a fake model. This harness measures the agent: how long a task takes, how much of that time the model, the tools, or nothing was busy, and whether async tool calls bought anything over Codex's mostly serial loop. It is ledger item P7: the first half, the harness; the improvement loop runs on its results.

1. [Run it](#run-it)
2. [Tasks](#tasks)
3. [What a run records](#what-a-run-records)
4. [Metrics and the report](#metrics-and-the-report)
5. [Cost](#cost)
6. [The test](#the-test)

Each run gets a fresh copy of its task's repository, committed to a new git repository, and a wall-clock limit. Both harnesses run with their own logins and no user configuration:

- uah: `uah exec --json --model M --effort E --config <scratch>/uah-<mode>.toml --state-dir <run>/uah-state`, with `UAH_HOME` in the scratch directory, so the owner's `config.toml`, `config.d` hooks, and history are not read or written. uah's Codex login is `$CODEX_HOME/auth.json`, which it reads in place; the harness copies no credential and never reads it.
- Codex: `codex exec --json --ephemeral --ignore-user-config --skip-git-repo-check -m M -c model_reasoning_effort=E`, with stdin closed. `--ephemeral` keeps the runs out of `~/.codex/sessions`.

A task with [follow-up prompts](#follow-up-prompts) runs on uah alone: `uah exec --stdin` takes them in the same session, and Codex has no way to without saving its session.

`-mode` sets the permission mode of both. `auto`, the default and the owner's everyday mode, has a reviewer model decide what needs approval: uah's `permission_mode = "auto"` (the generated configuration file holds only that key) and Codex's `--approve-for-me`, which implies the workspace-write sandbox. `workspace` refuses it: uah's `--sandbox workspace-write --ask never` and Codex's `-s workspace-write -c approval_policy="never"`. A task that needs the network or files outside the workspace passes only in `auto`.

Both load `~/.codex/AGENTS.md`, as both do by default. The environment drops `UAH_*`, `OPENAI_*`, `GO*`, and web-tty's variables, and sets `TMPDIR` to a shared directory in the scratch directory, which both sandboxes let commands write; the Go build cache lives there (`GOCACHE`), with `GOFLAGS=-count=1` so a slow suite is slow every time, `GOPROXY=off`, and `GOTOOLCHAIN=local`.

The uah binary is built from the working tree into the scratch directory at the start, unless `-uah` names one.
<!-- /memoria:section -->

<!-- memoria:section id="usage" files="main.go bench/run.go bench/dry.go" -->
## Run it

Run every command from the repository root.

```sh
go run ./tools/agentbench -list                     # the tasks, their tags, and what each exercises
go run ./tools/agentbench -dry                      # validate every task: no model calls, about a minute
go run ./tools/agentbench -tasks 'fix|slow' -effort low -repeat 1    # a subset on both harnesses
go run ./tools/agentbench -repeat 3 -parallel 2 -max-runs 150        # the full suite
go run ./tools/agentbench -report                   # rewrite the report from the results file
go run ./tools/agentbench -remeasure                # parse the saved runs again (after a parser or price change), then report
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `-tasks` | all | a regular expression over task names |
| `-harness` | `both` | `uah`, `codex`, or `both` |
| `-repeat` | 1 | runs of each task per harness |
| `-model` | `gpt-6.1-sol` | the model for both: uah's default on openai-codex, and in Codex's model list |
| `-effort` | `high` | the reasoning effort for both: `low` to `ultra` |
| `-mode` | `auto` | the permission mode of both: `auto` or `workspace` |
| `-parallel` | 1 | runs at once; keep it at 1 or 2 for the rate limits |
| `-timeout` | 15m | a run's wall-clock limit, unless its task sets `timeout` |
| `-max-runs` | 60 | refuse a plan with more runs to do than this |
| `-out` | `tools/agentbench/results/<model>-<effort>-<mode>.jsonl` | the results file |
| `-work` | `$TMPDIR/uah-agentbench` | the scratch directory |
| `-keep` | off | keep each run's workspace |
| `-uah-env` | none | `KEY=VALUE` added to uah's environment, after the variables the harness drops; repeatable; needs `-variant` |
| `-uah-config` | none | a top-level `key = value` line added to uah's generated configuration file; repeatable; needs `-variant` |
| `-variant` | none | a label for the uah runs, part of their results key; see [variants](#variants) |
| `-price-in`, `-price-cached`, `-price-out` | 1.25, 0.125, 10 | US dollars per million tokens, for the cost estimate |

The plan runs each repeat over every task, alternating which harness goes first, so neither always meets a warmer cache or a quieter hour. **Resume** is the default: a run whose key (task, harness, model, effort, repeat) is in the results file is skipped, except one that ended in `error` (the harness could not start it, for example). Stop with ctrl+c and start the same command again to continue. The results file and the run directories are in `tools/agentbench/results/`, which git ignores.

Each run appends one line to the results file and leaves a directory next to it, `<results>/<task>/<harness>-<model>-<effort>-<repeat>/`, with:

- `stream.jsonl`: the harness's JSONL events, each line prefixed with the time the harness read it and a tab;
- `stderr.txt`: its progress and errors;
- `timeline.json`: the parsed [timeline](#what-a-run-records);
- `diff.patch`: the agent's changes against the task's commit;
- `check.txt`: the check's output;
- `uah-state/` (uah only): the session files, the subagents' sessions, and each run's `stderr.log` diagnostics.

At the end the command writes `<results>.md`, the [report](#metrics-and-the-report).

### Variants

A variant is uah with something changed, such as a setting or a prompt, run against uah as it is (the control). `-variant NAME` labels the uah runs; the label is part of the results key, so the variant's runs and the control's share one results file without replacing each other, while Codex's runs keep their keys and are shared by both. `-uah-env KEY=VALUE` passes the change through the environment, and `-uah-config 'key = value'` through the configuration file the harness generates for uah (top-level keys only, before `permission_mode`; that variant's runs get a file of their own, `uah-<mode>-<variant>.toml`); each result records them in `env` and `uah_config`, and its directory is `uah+NAME-<model>-<effort>-<repeat>`. For adaptive effort at 2 steps:

```sh
go run ./tools/agentbench -harness uah -repeat 3                                                  # the control
go run ./tools/agentbench -harness uah -repeat 3 -uah-env UAH_ADAPTIVE_EFFORT=2-steps -variant adaptive2
```

The same variant through the configuration is `-uah-config 'adaptive_effort = "2-steps"' -variant adaptive2`. A prompt variant, for example, is `-uah-config 'model_instructions_file = "/tmp/uah-agentbench/prompts/runner.md"' -variant prompt-runner`. The report then has `uah+NAME` as a harness of its own in the per-harness tables, and a table of the variant against the control per task.
<!-- /memoria:section -->

<!-- memoria:section id="tasks" files="bench/task.go bench/fixture.go bench/dry.go bench/harness.go bench/run.go" -->
## Tasks

A task is a folder in `testdata/tasks/`:

| Path | Holds |
| --- | --- |
| `task.json` | `prompt` (what both harnesses get), `check` (a shell command; exit status 0 passes), `exercises` (one line), `tags`, and optionally `follow_ups` (below), `check_timeout` (default 3m), `timeout` (the run's limit), `solution_delete`, and the fixtures below |
| `repo/` | the repository the agent starts from |
| `solution/` | files laid over `repo/` that solve the task: the reference solution |
| `check/` | files laid over the agent's result before the check runs: hidden tests, and the original visible tests, so editing a test does not pass it |
| `home/` | with `fake_home`, the files of the run's own home directory |

A task may also need more than files. `setup` is a shell script run in the workspace after its first commit, with `TASK_DIR` set to the task's folder: to build git history from files the agent does not see, for example. `solution_script` runs after `solution/` is laid over the workspace, for a solution that is more than files. Both scripts run with a fixed git identity and git's background maintenance off, so they can commit on a machine without a git user and leave no lock files behind while the workspace is copied; a file that disappears during a copy is skipped. `service` is a shell command started in the task's folder for the whole run with `PORT` set, a local server (no two running services get the same port, even while one is still compiling); `{{URL}}` in the prompt and the check, and `SERVICE_URL` in every script, are its address. `fake_home` gives the agent, the scripts, and the check their own `HOME`, filled from `home/`, so a task may change files under `~` without touching the user's; `CODEX_HOME` then stays the user's, so the logins still work.

The agent sees only `repo/`. The check runs in a copy of the agent's result, so the workspace stays as the agent left it.

### Follow-up prompts

`follow_ups` lists more messages, sent in the same session one after another, each when the agent is done with the one before: "now also handle X", "rename what you just added". uah gets them through `uah exec --stdin`: the harness keeps uah's stdin open, writes the next follow-up as one line each time the stream reports `idle` (the run ended), and closes stdin after the last, so uah exits when that one is done. A follow-up is therefore one line. The check runs once, after the last.

Codex cannot take a follow-up without keeping its session: `codex exec` takes one prompt, and `codex exec resume` continues a session saved in `~/.codex/sessions`, which `--ephemeral` does not save and the harness keeps out of. So a task with follow-ups must carry the tag `uah-only` (`LoadTask` refuses it otherwise), and the plan has no Codex run for a `uah-only` task; in the report's per-task table its Codex columns are `0/0` and its wall ratio `-`.

**Validation.** `-dry` runs each task's check twice, with no model: on the untouched repository, where it must fail, and on the reference solution, where it must pass. A task that does not do both is not valid, and the command exits non-zero. Run it after any change to a task. It also warms the shared Go build cache.

To add a task: write `repo/` as a user's repository would be (a README, tests that a developer would have), a `prompt` as the user would type it, without naming the hidden tests, a `check` that is deterministic and needs no network, and the `solution/` that passes it. Name each README in `repo/` and `solution/` `README.fixture.md`: the harness copies it into the workspace as `README.md` (`copyFixture`, `TestFixtureReadmes`), and Memoria, which takes every `README.md` for a document of uah's, does not track it. Then run `go run ./tools/agentbench -dry -tasks '^<name>$'`.

The suite has 41 tasks. The tag `slow` marks a task whose tests take 10 to 60 s, where a good agent works while they run; `subagents` marks work that splits across subagents; `archetype` marks the ten taken from the kinds of work the owner's own sessions do most, as the mining of those sessions found them (P7), where the model's time, not the tools', is most of the wall time. Five are larger than the rest, as real sessions are: `large-repo` works in a vendored open-source project of a few hundred files where the agent must search for the place to change; `follow-up` marks several prompts in one session (and `uah-only`); `large-input` gives the agent megabytes to read; and `compaction` marks a session long enough to fill uah's context past its automatic compaction limit (90% of the model's window, about 245,000 tokens for gpt-6.1-sol). In the owner's sessions a compaction came only after 13 to 64 prompts and 145 to 414 model requests, so the long task is several prompts, each needing wide reading, in one context: a single prompt of logs did not do it, as the agent extracts what it needs with a script.

| Task | Tags | Exercises |
| --- | --- | --- |
| `ci-log-triage` | investigate, logs, large-input | 40 CI logs (2.7 MB, up to 110 KB each, generated by `gen.py` at setup) triaged into a CSV of causes, failing tests, and culprit commits, and a summary, past flakes that passed on a rerun and warnings after the tests |
| `curl-local-api` | network, escalation, archetype | Many curl calls against a local API (five pages, then details of each flagged item), which need an escalation out of the sandbox; then merged, sorted JSON |
| `curl-parallel-endpoints` | network, escalation, parallel | Four independent, slow endpoints of a local API snapshotted into one JSON object; the prompt says the sandbox has no network, so the model asks for the four escalated curl calls in one response and their auto-reviews can run at once |
| `fullstack-go-js` | go, javascript, feature, archetype | A filter across a Go HTTP API and a plain-JS front end, with tests on both sides |
| `git-changelog-two-repos` | git, docs, archetype | Release notes between two tags of two local repositories, from conventional commit subjects |
| `go-add-tests` | go, tests | Tests for an untested ring buffer to at least 80% coverage; the check restores the source and reads `go test -cover` |
| `go-api-migration` | go, refactor | A deprecated logging helper's 14 call sites in 4 files moved to a structured API, then the helper deleted |
| `go-branch-review` | go, review, subagents, archetype | A review of a branch against `main` that must find three planted bugs in three files |
| `go-bug-hunt` | go, investigate | A symptom only (totals a cent low on discounted orders); the cause among about 9 files of rounding helpers |
| `go-cli-exit-codes` | go, fix | A CLI's exit codes and its stdout/stderr split made to match the contract in its README |
| `go-config-env-override` | go, feature, docs | The precedence of a layered configuration (flags over environment) fixed, and a new key through file, environment, flags, and README |
| `go-data-race` | go, fix, concurrency | A counter made safe for concurrent use; the check runs `go test -race` |
| `go-docs-and-code` | go, feature, docs | A `retries` setting through configuration and the fetch loop, documented in the README |
| `go-errors-sentinel` | go, refactor | Sentinel errors from `docs/errors.md`, wrapped through three layers and matched with `errors.Is` |
| `go-followup-flag` | go, feature, follow-up, uah-only | Three prompts in one session: a `--since` time filter for a log CLI, then relative durations for it, then a rename to `--after` with a deprecated alias |
| `go-fix-failing-tests` | go, fix | Three independent bugs in three files behind one failing suite |
| `go-ini-parser` | go, feature | A half-finished INI parser completed to the rules in its README |
| `go-investigate-answer` | go, investigate | A question with no code change: which function drops a record, written to `ANSWER.txt` |
| `go-large-repo-guide` | go, docs, investigate, large-repo, follow-up, uah-only, long, compaction | Seven prompts in one session over smithy-go (the repository of `go-large-repo-bug`, without its bug, copied in by `setup`): an `ARCHITECTURE.md` of every package, then sections on the middleware stack, the encoders, the transport, documents, auth and waiters, and verified gotchas; wide reading in one context, enough for uah's automatic compaction: in the smoke run the context reached 254,000 tokens in the seventh prompt and compacted once (four prompts had reached 165,000 and did not), so a run that reads less may finish without one |
| `go-large-repo-bug` | go, investigate, fix, large-repo | A broken URL path reported from a client, found and fixed in a vendored [smithy-go](https://github.com/aws/smithy-go) v1.22.4 (242 files, 27,000 lines of Go): a path-label replacement that loses the rest of the path when a value outgrows its placeholder's spare capacity |
| `go-multi-module` | go, fix, workspace | A `go.work` of three modules, each with a bug; independent builds and tests |
| `go-multifile-feature` | go, feature | `-format json` for a CLI: flag, formatter, and wiring |
| `go-perf-quadratic` | go, performance, slow | A quadratic dedupe made fast for 200,000 IDs without changing its semantics; the untouched code takes 20 s to time out |
| `go-py-fixtures` | go, python, feature | A new column through a Python fixture generator, its golden files, and the Go loader |
| `go-plan-handoff-implement` | go, feature, docs, follow-up, uah-only, archetype | The owner's plan-then-build shape in three prompts: a design question with no code change, a handoff file, then a file-backed store behind `-data` that survives a kill |
| `go-race-tests` | go, fix, concurrency, archetype | Three data races in three packages under `go test -race`, fixed without changing the tests |
| `go-rename-refactor` | go, refactor | A type and its constructor renamed across 14 files in 5 packages and the README |
| `go-repo-overview` | go, docs, investigate, archetype | An `ARCHITECTURE.md` for a 15-file service: packages, the request flow, retries, and where to add a feature |
| `go-slow-60s` | go, slow, fix, feature, archetype | A one-minute suite, a fast failing package, and an independent feature |
| `go-slow-build-script` | go, slow, generate | A 10 s generator script: edit its data and the code, then regenerate |
| `go-slow-suite` | go, slow, feature | A failing package in a 27 s suite, and an independent feature from `docs/` |
| `go-slow-two-packages` | go, slow, fix | Two packages with 20 s of tests and one failure each; run at once, they take half the time |
| `go-spec-implement` | go, feature | A size parser and formatter from a precise `docs/SPEC.md` |
| `go-subagent-audit` | go, fix, subagents | Four independent bugs in four packages, described in the prompt |
| `go-vet-fixes` | go, fix | Six `go vet` findings across two packages, with hidden tests so deleting code does not pass |
| `home-config-surgery` | config, escalation, archetype | A hook removed from a tool's configuration under `~` (a fake home) among look-alikes; writing there needs an escalation |
| `md-findings-report` | docs, investigate, archetype | A 10 to 25 KB findings document from eight sources about one outage, with numbers from a CSV |
| `md-revise-handbook` | docs, edit, archetype | Five edits to a 38 KB handbook in one prompt: a rename outside code blocks, a new section, renumbering, a table, a removal |
| `node-fix-module` | javascript, fix | A small ES module package whose `node --test` fails in three files |
| `py-log-parser` | python, investigate | A symptom only (durations over an hour are wrong), traced through a Python log parser |
| `py-slow-tests` | python, slow | A Python suite with 20 s of sleeps, a bug fix, and a new function from `docs/` |

Python tasks, the generator and check of `ci-log-triage`, and the check of `go-large-repo-guide` need `python3`, the JavaScript task needs `node`; neither needs a package.

`go-large-repo-bug`'s repository is aws/smithy-go v1.22.4 as published, under the Apache License 2.0 (its `LICENSE` and `NOTICE` are in the repository), with one change, the planted bug: three lines removed from `encoding/httpbinding/path_replace.go` (`solution/` restores them). [THIRD_PARTY_NOTICES.md](../../THIRD_PARTY_NOTICES.md) records it; the file itself carries no notice, which would point the agent at the bug.
<!-- /memoria:section -->

<!-- memoria:section id="timeline" files="bench/timeline.go bench/parse_uah.go bench/parse_codex.go" -->
## What a run records

Both streams become one `Timeline` (`timeline.json`), with times in milliseconds from the moment the harness started the process:

| Field | Holds |
| --- | --- |
| `requests` | each model request: `start_ms`, `first_byte_ms` (when known), `end_ms`, `tokens` (`input`, `cached`, `output`, `reasoning`), `tool_calls` it issued, `effort`, `stop` (`complete`, or how it was cut off), `text_bytes` (the assistant text it wrote), and `agent` (a subagent's session ID, or empty for the main agent) |
| `calls` | each tool call: `name`, `kind` (`tool`, `wait`, or `agent`), `args` (a one-line summary), `args_bytes`, `escalated` (it asked to run outside the sandbox), `request` (the index of the request that issued it), `issued_ms`, `start_ms`, `end_ms`, `ok`, `detail` (an exit status), and `agent` |
| `turns` | user turns: one per prompt, follow-ups included |
| `compactions` | uah's context compactions: `start_ms`, `end_ms` (the summary call), `trigger` (`auto` when the context reached the limit), `tokens` (the context then), and `error` if it failed. Codex's events show none |
| `tokens` | the run's totals; input includes cached, output includes reasoning |
| `inferred` | what was estimated rather than read |
| `answer`, `errors` | the final message, and errors the stream reported |

**uah** reports everything. A request ends at `model_responded`, which gives its `stop`, and starts `duration_ms` before it; its effort is the last `effort` of `run_started` or a settings `control_input`; its first byte is the matching `model_attempt` line in the run's `stderr.log` (the HTTP first byte), else the first streamed text. A call is issued at `tool_called`, starts at `tool_started`, and ends at `tool_finished`. Subagents do not appear in the main stream: the parser reads every other session file in the state directory with [`internal/sessionfile`](../../internal/sessionfile/README.md), where a request runs from its `turn` record to its `model_response`, and a call from the response that issued it to its last `tool_call_status`. This follows the runner's own trajectory adapter (`benchmarks/harbor` in unreal-agent), which orders asynchronous results by sequence, time, and the turn they arrived before.

**Codex** reports neither model requests nor per-request tokens, and its events carry no times. The harness stamps each line as it reads it (Codex writes a line per event), and the parser infers requests: the model is busy from a turn's start, or from the end of the last running command, until the next command starts or the turn completes. A gap shorter than 300 ms is not a request but Codex running the next call of the same response, one after another. A patch (`file_change`), which has no start event, ends the request that issued it. A command that starts while another still runs (Codex hands the model a long command before it ends) follows a request from the last event to it, so Codex's overlap is counted where its events show it. Tokens are the turns' totals (`inferred` is `["requests", "request_tokens"]`). Codex's model time is an estimate: a request that ends in a message with no call, while a command runs, is not seen.
<!-- /memoria:section -->

<!-- memoria:section id="metrics" files="bench/timeline.go bench/behavior.go bench/report.go bench/report_variant.go" -->
## Metrics and the report

Each result line holds the run's `metrics`:

| Metric | Meaning |
| --- | --- |
| `wall_ms` | the process's wall time |
| `model_ms` | time at least one model request, or a compaction's summary call, was in flight |
| `tool_ms` | time at least one tool call ran; waits are not work |
| `overlap_ms` | time a request and a tool call ran at once: what async scheduling gains |
| `model_only_ms`, `tool_only_ms`, `idle_ms` | with `overlap_ms`, a split of the wall time: the critical path was the model alone, the tools alone, both, or neither (startup, the harness, a wait on nothing) |
| `wait_ms` | time a wait call blocked while nothing else ran |
| `first_byte_ms` | the median time to a request's first byte |
| `longest_call_ms`, `longest_call` | the longest tool call and its arguments |
| `turns`, `requests`, `tool_calls`, `subagents` | counts |
| `max_concurrent`, `avg_concurrent` | the most tool calls running at once, and the mean while any ran |
| `tokens`, `cost_usd` | the totals and their [estimated cost](#cost) |
| `behavior` | where the model's time goes, below |

The mining of the owner's sessions (P7) found the model is about 89% of the wall time, so `behavior` counts what makes requests more or longer:

| Field | Meaning |
| --- | --- |
| `output_reasoning`, `output_patch`, `output_tool_args`, `output_text` | the output tokens split by what they wrote: reasoning as reported, the rest in proportion to the bytes of patches (`apply_patch`, `Edit`, `Write`), of other tools' arguments, and of text. Codex reports tokens per turn, and its patch events hold paths, not hunks, so its split is rough |
| `efforts` | requests per effort. A uah request's effort is its own, from the `effort` of its `model_attempt` line in the run's `stderr.log`, which with [adaptive effort](../../internal/engine/README.md#adaptive-effort) also gives `effort_reason`, kept in the timeline's request; without that line, the session's effort |
| `ritual_requests`, `ritual_ms` | the main agent's first requests that only load a skill (`SkillUse`) or read an instructions file (`AGENTS.md`, `RTK.md`, `CLAUDE.md`), and the time until the next request |
| `patch_then_verify` | requests that only patched, followed by a request that runs a command: a build or a test that could have gone with the patch |
| `escalations`, `escalations_refused`, `review_ms`, `review_median_ms` | calls that asked to run outside the sandbox (uah's `sandbox_permissions: require_escalated`), how many failed, and the time from issuing them to starting them, which is the approval's latency. Codex's events show neither, so its counts are 0 |
| `approval_waits`, `approval_wait_ms` | every call that started 300 ms or more after it was issued, which in uah means it waited for an approval (an escalation, or a patch outside the workspace), and the total wait |
| `aborted`, `aborted_ms` | requests that did not complete (canceled, failed, or cut off by the limit) and the model time they took |
| `compactions`, `compaction_ms` | context compactions and the time their summary calls took (uah only) |
| `same_effort_*`, `changed_effort_*` | the main agent's requests after its first, at the same effort as the request before them or at another: `_requests`, `_input` and `_cached` tokens, and `_cache_ratio`, the cached share of their input (0 with none). Whether the prompt cache holds when the effort changes |

The report has, per model and effort: a table per harness (runs, pass rate, medians of the times and counts, token totals, total cost); a table per harness of `behavior` summed over its runs; a table per task with uah against Codex (pass counts, median wall times and their ratio, uah's overlap, each one's most concurrent calls, median input tokens, median cost); for each [variant](#variants), a table per task and over all its runs against the control (pass counts, median wall times and their ratio, requests, output tokens, and patch tokens); and every run, with its status, the split of its wall time, its longest call, and the size of its diff. A run's status is `done` (exit 0), `failed` (non-zero), `timeout`, or `error`; a timed-out run does not pass, even if its check does.
<!-- /memoria:section -->

<!-- memoria:section id="cost" files="main.go bench/timeline.go" -->
## Cost

The estimate is uncached input, cached input, and output tokens at the `-price-*` rates, $1.25, $0.125, and $10 per million by default. Those are placeholders: no price for gpt-6.1-sol is published to the harness, and both CLIs here run on a ChatGPT login, where runs use the plan's limits rather than money. Use the token counts for comparisons; set the rates when an API price applies.

As a scale, each smoke pass (two tasks, both harnesses, effort low, 4 runs) took 40 to 95 s a run, about 510,000 input tokens (85% cached) and 3,000 to 5,000 output tokens in all: under $0.20 at the default rates. A full pass of 35 tasks × 2 harnesses × 3 repeats is 210 runs (raise `-max-runs`): at low effort about 27 million input tokens, $10, and 4 hours at `-parallel 1`; at high effort expect two to four times the tokens and the time.
<!-- /memoria:section -->

<!-- memoria:section id="test" files="bench/bench_test.go bench/variant_test.go bench/followup_test.go bench/testdata/uah.jsonl bench/testdata/codex.jsonl" -->
## The test

`go test ./tools/agentbench/...` makes no model calls. It parses a recorded uah stream and a stamped Codex stream of the same prompt (two commands in parallel, then an answer) and checks the requests, calls, tokens, and metrics; checks the metrics' arithmetic and the `behavior` counts, the cache split by effort included, on synthetic timelines; reads a request's effort and its reason from a `model_attempt` line; loads every task; dry-runs every task not tagged `slow` (about 5 s with a warm build cache, which it keeps in `$TMPDIR/uah-agentbench-test`; `-short` skips it); checks the plan's order, a variant's keys, its configuration file, and the report's variant table; checks that a `uah-only` task has no Codex run and that follow-ups need that tag; runs a task with two follow-ups against a fake `uah` script that answers each message and goes idle, to see each sent after the run before it ended; and parses compactions from a synthetic stream.
<!-- /memoria:section -->

## History

`history/` keeps the results of each experiment worth keeping, one JSON line per run, with local paths shortened to `~` and `$TMPDIR`. [Agent tuning](../../docs/design/agent-tuning.md) explains each file and what was decided from it. Copy a results file there after an experiment and add its section to that record.
