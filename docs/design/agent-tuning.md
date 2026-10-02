# Agent tuning: uah against Codex

This record collects every measurement made with the agent benchmark ([`tools/agentbench`](../../tools/agentbench/README.md)), what each experiment changed, and what was decided. It is the source for the performance part of release notes. Each experiment's raw results are in [`tools/agentbench/history`](../../tools/agentbench/history), one JSON line per run (local paths are shortened to `~` and `$TMPDIR`).

## Contents

1. [How runs are measured](#how-runs-are-measured)
2. [Where the time goes](#where-the-time-goes)
3. [Baseline](#baseline)
4. [Experiment 1: freeform `apply_patch`](#experiment-1-freeform-apply_patch)
5. [Experiment 2: async prompts](#experiment-2-async-prompts)
6. [Experiment 3: wake policies](#experiment-3-wake-policies)
7. [Experiment 4: cutting turns](#experiment-4-cutting-turns-quick-round)
8. [The full suite, 10 repeats](#the-full-suite-10-repeats)
9. [Decisions](#decisions)
10. [Still running and next](#still-running-and-next)
11. [For release notes](#for-release-notes)

## How runs are measured

- Each run gives `uah exec` or `codex exec` one coding task in a fresh copy of a small repository and checks the result automatically; 35 tasks, each valid only if its check fails on the untouched repository and passes on a reference solution.
- Both harnesses use the same model and effort (gpt-6.1-sol, high), the ChatGPT login, auto mode (auto-review for escalations), the workspace-write sandbox, and the owner's AGENTS.md, RTK.md, and skills. uah gets a throwaway home and state directory per run; Codex runs with `--ephemeral --ignore-user-config`.
- Numbers are medians over a task's repeats; totals are sums of per-task medians. The same task varies by 20–30% between runs, so a difference under about 10% on one task is noise, and a sustained 20%+ over many tasks is real.
- Codex's JSON stream has no per-request timing, so its model time and request count are inferred from the gaps between its commands.

## Where the time goes

Mining the owner's real sessions (243 uah runs, 12.2 hours; 382 Codex tasks) before any benchmark:

- The model is 89% of wall time; tools alone 5%; uah's own overhead is negligible (dispatch 10 ms, wake 4 ms).
- Patch text was 37% of all output tokens, reasoning 38%; high-effort turns were half of all model time.
- 37% of runs spent the first turn on a ritual (loading the always-on skill, reading RTK.md).
- Async tool calls worked as designed (42% of tool time overlapped the model), but the model rarely had useful work to overlap.

So the gains come from fewer and cheaper model turns, not from more tool parallelism.

## Baseline

2026-10-01, 35 tasks × 2 harnesses × 3 repeats, 210 runs ([raw](../../tools/agentbench/history/2026-10-01-baseline.jsonl)).

| | uah 1.6.1 | Codex 0.159.3 |
| --- | ---: | ---: |
| Passed | 104/105 | 103/105 |
| Median wall per task | 120 s | 104 s |
| Total wall | 252 min | 216 min |
| Model requests | 808 | 664 |
| Input tokens | 16.6M | 18.3M |
| Output tokens | 404k (62% patch text) | 342k |
| Estimated cost | $8.64 | $8.51 |

- uah's patches dominated its output: one 5-edit revision of a 38 KB markdown file took 397 s (Codex 107 s) because the model rewrote the whole file in one JSON-escaped patch.
- uah made 125 "patch, then verify in a new turn" pairs against Codex's 43, and spent 20 requests on the startup ritual.
- uah was already faster on the local-API, home-config, branch-review and slow-build tasks, and used fewer input tokens on most tasks.

## Experiment 1: freeform `apply_patch`

uah offered `apply_patch` as a JSON function (`{"input": "*** Begin Patch\n…"}`), so every newline and quote was escaped. Codex rust-v0.159.1 offers only a freeform tool: a Responses API custom tool with a Lark grammar, whose input is the raw patch. The runner fork gained custom tools (v0.4.0) and uah switched behind a flag. 10 edit-heavy tasks × 3, against a control run at the same time ([raw](../../tools/agentbench/history/2026-10-02-exp1-freeform-prompts.jsonl)).

| | JSON function | Freeform | Change |
| --- | ---: | ---: | ---: |
| Total wall | 112 min | 85 min | −24% |
| Output tokens | 201k | 149k | −26% |
| Patch tokens | 148k | 90k | −39% |
| Requests | 229 | 215 | −6% |
| Passed | 30/30 | 30/30 | |

Faster on 8 of 10 tasks; the markdown revision went from 397 s to 106 s (Codex 107 s). With the freeform tool offered, the model also chose scripts over patches where that was faster.

Against Codex on the same 10 tasks: Codex 1785 s, uah before 2158 s, uah freeform 1710 s; output tokens 50.5k / 64.2k / 49.7k.

## Experiment 2: async prompts

The 6 slow tasks (test suites and builds of 10–60 s) × 3, against a control (same raw file as experiment 1).

| | Default prompt | Runner's own prompt | Default + "wait, don't poll" |
| --- | ---: | ---: | ---: |
| Total wall | 37.1 min | 33.7 min (−9%) | 35.6 min (−4%) |
| Model time | 34.5 min | 28.7 min (−17%) | 32.4 min (−6%) |
| Requests | 164 | 149 | 148 |

Prompts alone did not stop the model from polling a running command (`ps`, `sleep 20`, `git status`); the runner woke it on every finished call and for "still running" placeholders, and it filled the wait with cheap commands.

## Experiment 3: wake policies

Five runner-level wake policies, each behind a switch in the runner fork (v0.5.0-rc.1). Quick round: the 6 slow tasks once each ([raw](../../tools/agentbench/history/2026-10-02-wake-quick.jsonl)). Proper round: the three best plus Codex's 30 s foreground cap, 6 slow tasks × 3, with a fresh control on the freeform-only build, plus an edit check on 4 tasks ([raw](../../tools/agentbench/history/2026-10-02-wake-proper.jsonl)).

Proper round, slow tasks (totals of medians):

| | Codex | uah control | No placeholder wakes | Foreground 5 min | Debounce 2 s | Foreground 30 s |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Wall | 663 s | 650 s | 652 s | 662 s | 686 s | 660 s |
| Model time | 515 s | 594 s | 483 s (−19%) | 495 s | 570 s | 505 s |
| Requests | 35 | 53 | 40 (−25%) | 43 | 45 | 41 |
| Output tokens | 14.3k | 14.9k | 12.7k | 12.5k | 14.9k | 12.9k |
| Estimated cost | $0.44 | $0.42 | $0.34 (−19%) | $0.38 | $0.36 | $0.40 |

- **No placeholder wakes**: a turn's results are delivered when every call it issued has finished; the model is never woken only to hear that a call is still running. On the 50 s suite, requests went from 11 to 6 (Codex 5).
- Wall time did not move: the model already overlapped the long tests, so the gain is model work and cost.
- Edit check (4 tasks × 3): control 459 s, no placeholder wakes 471 s, same requests and tokens; neutral. Codex 517 s.
- Wake-when-all-done never triggered on these tasks; a prompt rewrite for async made things worse (+7% wall).

### Wake variations, 8 repeats

5 tasks (the 50 s and 27 s suites, two slow packages, the slow algorithm, a bug hunt) × 8 repeats per variant, 120 runs, all passed ([raw](../../tools/agentbench/history/2026-10-02-wake-abc.jsonl)).

| | A: hold, 5 min valve | B: 60 s valve | C: release quick results after 10 s |
| --- | ---: | ---: | ---: |
| Wall | 598 s | 594 s | 635 s (+6%) |
| Model time | 451 s | 448 s | 556 s (+23%) |
| Requests | 37.5 | 37.5 | 45 |
| Estimated cost | $0.29 | $0.29 | $0.36 |

With 8 repeats, a task's middle half spans about ±10% of its median (the 50 s suite: 128–145 s), so differences under 10% between variants are noise.

## Experiment 4: cutting turns (quick round)

Three switches, 8 tasks × 1 run each against a control, all passed ([raw](../../tools/agentbench/history/2026-10-02-turn-cutting-quick.jsonl)). One run per task, so these are directions, not results.

| | Wall | Requests | Output tokens | Cost |
| --- | ---: | ---: | ---: | ---: |
| Control | 962 s | 60 | 26.0k | $0.58 |
| Lower effort for turns that only react to tool results | 845 s (−12%) | 56 | 21.4k (−18%) | $0.55 |
| Primed first turn (layout, git status, AGENTS.md includes) | 942 s (−2%) | 56 | 24.9k | $0.54 (−7%) |
| Automatic compile check after an edit | 1104 s (+15%) | 65 | 29.5k | $0.65 |

Lower effort costs about 3 points of prompt-cache hits (the effort level appears to be part of what the cache matches), which gives back part of the saving.

## The full suite, 10 repeats

35 tasks × 10 repeats × 5 groups, 1,750 runs at 65 concurrency, all groups interleaved ([raw](../../tools/agentbench/history/2026-10-02-big-35x10.jsonl)). uah is the build with freeform `apply_patch` and the wake default (runner fork v0.5.1); totals are sums of per-task medians.

| | Codex 0.159.3 | uah default | uah + lower effort | uah + lower effort + primed first turn | uah + corrected preamble |
| --- | ---: | ---: | ---: | ---: | ---: |
| Passed | 348/350 | 344/350 | 342/350 | 342/350 | 344/350 |
| Wall | 4172 s | 4223 s (+1%) | 3431 s (−18%) | **3270 s (−22%)** | 4232 s |
| Model time | 3949 s | 3994 s | 3192 s | 3054 s | 4014 s |
| Requests | 222 | 256 | 237 | 231 | 246 |
| Output tokens | 114k | 111k | 86k | 81k | 113k |
| Estimated cost | $2.90 | $2.50 (−14%) | $2.10 | **$2.00 (−31%)** | $2.50 |
| Cached input | 85.7% | 87.2% | 86.8% | 86.4% | 86.0% |
| Faster than Codex on | | 13 of 35 tasks | | 33 of 35 tasks | |

- uah's default (freeform patch and the wake policy) is level with Codex on time and 14% cheaper; with lower effort for follow-up turns and a primed first turn it is 22% faster and 31% cheaper, and faster on 33 of 35 tasks.
- The corrected preamble changed nothing measurable.
- Pass rates: the extra failures are concentrated in two tasks, each failing the same way in every harness. The branch review misses the swallowed `Record` error (Codex 8/10, uah 5–6/10), and the findings report keeps a red herring (Codex 10/10, uah 7–9/10). Both are review-quality misses, not regressions from the switches; the branch review is uah's weak spot against Codex.

## Decisions

| Date | Decision | Ledger |
| --- | --- | --- |
| 2026-10-02 | `apply_patch` is a freeform tool only, as in Codex; old sessions' JSON calls are still read and replayed | [90](../ledger.md) |
| 2026-10-02 | No placeholder wakes is the default, with a 5-minute safety valve (a call still running after 5 minutes wakes the model with its output so far); debounce, wake-when-all-done and both foreground variants were removed | [91](../ledger.md) |
| 2026-10-02 | The prompt stays Codex's adapted prompt; prompt-only async changes did not pay off | |
| 2026-10-02 | The wake valve stays at 5 minutes (A); a 60 s valve (B) measured the same and releasing quick results early (C) was worse | [91](../ledger.md) |
| 2026-10-02 | Automatic checks after an edit are dropped: the model still ran the tests after each edit, so the check added work | |

## Still running and next

- The whole suite (35 tasks) × 10 repeats at 64 concurrency: Codex; uah's default; uah with lower effort for follow-up turns; uah with lower effort and a primed first turn; uah with a preamble that describes the new wake policy.

## For release notes

- uah's `apply_patch` is now a freeform tool, as in Codex: on edit-heavy tasks −24% wall time and −26% output tokens, and uah is now faster than Codex on those tasks (1710 s against 1785 s over 10 tasks).
- The model is no longer woken just to hear that a command is still running: on long builds and test suites, −25% model requests and −19% model time and cost, with a 5-minute safety valve for commands that never end.
- With both, uah matches Codex on wall time on slow tasks and is cheaper (estimated $0.34 against $0.44 on the 6 slow tasks).
