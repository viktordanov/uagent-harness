# Sandboxing and approvals: plan

Status: decided 2026-09-24, not built. The research and the options are in [sandbox-research.md](sandbox-research.md); Codex facts are from openai/codex at rust-v0.156.1.

The rule for every choice below: do what Codex does, unless uah's runner forces a difference.

1. [Decisions](#decisions)
2. [How a command runs](#how-a-command-runs)
3. [Packages and files](#packages-and-files)
4. [Configuration](#configuration)
5. [Phases](#phases)
6. [Tests](#tests)

## Decisions

| # | Decision | Choice | Codex |
| --- | --- | --- | --- |
| S1 | Mechanism | The embedded engine's own Bash translator. The process engine gets a `SHELL` shim that sandboxes only; it cannot ask for escalation | The same model, per command |
| S2 | Default mode | `workspace-write`: the workspace, `/tmp`, and `$TMPDIR` are writable; the rest of the disk is readable | Workspace-write for trusted projects |
| S3 | Network for commands | Off. An escalated command runs outside the sandbox, with network | `network_access = false` |
| S4 | Protected paths | `.git`, `.uagent`, `.agents`, and `.codex` stay read-only inside writable roots, so `git commit` escalates | `.git`, `.agents`, `.codex` |
| S5 | Rules | Codex's `prefix_rule` syntax in `~/.config/uagent/rules/*.rules` and, for trusted projects, `<workspace>/.uagent/rules/*.rules`. The strictest matching decision wins | The same, in `~/.codex/rules` and `.codex/rules` |
| S6 | What `allow` means | Run without asking and outside the sandbox | The same |
| S7 | Approval policy | `on-request` (default) and `never`. A denial goes back to the model, which re-runs the command with `sandbox_permissions: "require_escalated"` and a `justification`; uah never retries by itself | The same; `untrusted` is internal only |
| S8 | Auto-review | On by default, TUI and headless. It fails closed (deny) and stops after 3 denials in a row, then asks the user (headless: denies) | Opt-in `approvals_reviewer = "auto_review"` (on in the owner's Codex config) |
| S9 | Reviewer model | `codex-auto-review` at low effort on openai-codex; the session model at low effort on other providers | `codex-auto-review`, or `gpt-5.6-luna` with an API key |
| S10 | Approval prompt | Codex's: "Yes, proceed", "Yes, and don't ask again for commands that start with `<prefix>`" (when a prefix rule is proposed), and "No, and tell the agent what to do differently" | The same three |
| S11 | Secrets | Commands inherit the full environment. `[shell_environment_policy]` offers Codex's `inherit`, `exclude`, `include_only`, `set`, and `ignore_default_excludes = false` for the `*KEY*`, `*SECRET*`, `*TOKEN*` filter. The sandbox can read the whole disk | Inherits everything by default (`ignore_default_excludes` defaults to true) |
| S12 | Linux | The system `bwrap`. Without it, uah warns and asks for every command that no rule allows | Bundled or system bwrap |

## How a command runs

On the embedded engine, uah registers its own Bash tool. Its schema is the runner's plus two optional fields, `sandbox_permissions` (`"use_default"` or `"require_escalated"`) and `justification`, so the model can ask up front, as in Codex.

For each call, `Translate`:

1. Runs `PreToolUse` hooks (M6).
2. Checks the rules. `forbidden`: the call fails with the rule's justification. `allow`: it runs unsandboxed without asking.
3. For a call that does not ask for escalation: runs it sandboxed. The model sees a denial as a normal failure, plus one line: `uah: the sandbox blocked this (workspace-write); re-run with sandbox_permissions "require_escalated" and a justification if it is needed`.
4. For an escalation: asks the approver (auto-review first, then the user if the reviewer is unsure or off), then runs unsandboxed or fails with the reason.

A sandboxed call is a normal shell operation whose shell is `uah __sandbox --mode <mode> --workspace <dir> -- <real shell>`: `sandbox-exec -p <policy>` on macOS, `bwrap <args>` on Linux. Output files, cancellation, and orphan cleanup stay the runner's. Asking blocks the coordinator while the prompt is open, as the agent in Codex waits; the runner's other tool calls keep running.

On the process engine, `SHELL` points at the same `uah __sandbox`, and commands only run sandboxed; there is no way to ask for more than the rules allow.

## Packages and files

| File | Contents |
| --- | --- |
| `internal/sandbox/mode.go` | `Mode` (read-only, workspace-write, danger-full-access), writable roots, protected paths |
| `internal/sandbox/seatbelt.go` | The macOS policy, ported from Codex's `seatbelt_base_policy.sbpl` and write rules (Apache-2.0, with its notice) |
| `internal/sandbox/bwrap.go` | The Linux bubblewrap arguments |
| `internal/sandbox/denied.go` | Codex's denial heuristic (exit code plus "operation not permitted" and similar text) |
| `internal/sandbox/env.go` | `shell_environment_policy` |
| `cmd/uah/sandbox.go` | The hidden `uah __sandbox` entry point |
| `internal/rules/` | A parser for the `prefix_rule` subset of Codex's Starlark rules, and matching with the strictest decision winning |
| `internal/approval/` | The approver: policy, rules, the reviewer, the user, and the circuit breaker. Session events `ApprovalRequested` and `ApprovalResolved` |
| `internal/review/` | The reviewer (built as its own package, not `internal/approval/review.go`): user messages (trusted), the last tool calls without their output (untrusted), the action, and a fixed policy prompt. JSON out: outcome, risk, reason. See [Auto-review: as researched](#auto-review-as-researched) |
| `internal/engine/embedded/bash.go` | The Bash translator with the extra schema fields |
| `internal/tui/state`, `render` | The approval overlay and "auto-approved: <reason>" lines |

## Configuration

```toml
sandbox_mode = "workspace-write"     # read-only, workspace-write, danger-full-access
approval_policy = "on-request"       # on-request, never
approvals_reviewer = "auto_review"   # auto_review or user

[sandbox_workspace_write]
network_access = false
writable_roots = []

[shell_environment_policy]
inherit = "all"                      # all, core, none
ignore_default_excludes = true       # false drops *KEY*, *SECRET*, *TOKEN*
exclude = []
set = {}
```

The names match Codex's `config.toml`, so settings carry over. `--sandbox <mode>` and `--ask <policy>` override them, and the TUI's `/sandbox` shows the mode.

## Phases

| Phase | Scope | Estimate |
| --- | --- | --- |
| 1 | `internal/sandbox`, `uah __sandbox`, the embedded Bash translator (sandboxed calls, denial hint), the process engine shim, configuration, `/sandbox` | 4–5 days |
| 2 | Escalation with the user prompt, `internal/rules`, "don't ask again" writing a prefix rule to `~/.config/uagent/rules/default.rules` | 3–4 days |
| 3 | The auto-reviewer, its events, and the circuit breaker | 2–3 days |

Each phase ships on its own: after phase 1, commands are sandboxed and escalations are denied with a reason.

## As built (phase 1)

- **No `uah __sandbox`.** A per-session script execs `sandbox-exec` or `bwrap` around the real shell, and both engines use it as the shell: the embedded engine in its Bash translator, the process engine as the runner's `SHELL` (uagent v0.4.1's `RunnerBackend.Env`). Inherited environment variables are referenced as `"$NAME"` in it, so no value is written to disk.
- **Seatbelt** follows Codex, plus one fix: every writable root excludes the protected paths of all roots, so a workspace under `$TMPDIR` keeps `.git` read-only. Overhead is about 7–9 ms per command.
- **bubblewrap** follows Codex without its seccomp helper; `--unshare-net` isolates the network. A missing protected name is not protected on Linux (Codex creates an empty mount point on the host, which breaks git in subdirectories of a repository and races with parallel commands). CI installs bwrap and runs the real tests.
- **Denials** use Codex's keywords plus DNS and "network is unreachable" messages, because bwrap's network namespace fails that way.
- **go build** works in workspace-write: Go ignores cache writes it cannot make, so builds are slower rather than broken. `writable_roots = ["~/Library/Caches/go-build"]` restores the cache.

## Tests

- **Policy tables.** The generated Seatbelt profile and bwrap arguments per mode, compared with golden files.
- **Real sandbox runs on macOS.** Each mode against real commands:
  - writes inside and outside the workspace;
  - `.git` writes;
  - network access;
  - output files.
- **Linux.** bwrap in CI; GitHub runners need the AppArmor setting Codex's CI uses.
- **Embedded engine.** `fakellm` scripts through denial, escalation request, approval, and the "don't ask again" rule.
- **Reviewer.** Its prompt as a golden file, and its behaviour on allow, deny, bad JSON, and timeout, against a fake model.

## Auto-review: as researched

Status: the reviewer is built (`internal/review`) and configured; lane A's approver wires it in. Codex facts are from openai/codex at rust-v0.156.1; `CX` is `codex-rs`.

### What Codex does

| Topic | Codex behaviour | Where |
| --- | --- | --- |
| When it runs | Instead of the user prompt, for escalations, network approvals, MCP calls, patches, and permission requests, with `approvals_reviewer = "auto_review"` and approval policy `on-request` or `granular` | `CX/ext/guardian-reviewer/src/routing.rs` |
| Model | `codex-auto-review` (`DEFAULT_APPROVAL_REVIEW_PREFERRED_MODEL`) when the model catalog lists it, which it does on ChatGPT sign-in; `gpt-5.6-luna` with an API key; else the session model. A model may carry its own `auto_review_model_override` | `CX/model-provider/src/provider.rs:124`, `CX/ext/guardian-reviewer/src/model.rs`, `CX/core/tests/suite/guardian_review.rs:779` |
| Effort | `low` when the chosen model supports it, else the model's default | `model.rs` (`select_review_model`) |
| How it runs | A read-only, no-network sub-session. It may run read-only commands (`exec_command`, `write_stdin`, `view_image`, `exec`, `wait`) before answering. Approval policy `never`. The answer is constrained by `final_output_json_schema` | `CX/ext/guardian-reviewer/src/settings.rs` |
| System prompt | `policy_template.md` with `policy.md` in place of `{{ tenant_policy_config }}`, then the output contract. `[auto_review] policy` replaces `policy.md`; a template can be configured too | `CX/prompts/templates/guardian/`, `CX/prompts/src/guardian_instructions.rs`, `CX/core/src/guardian/reviewer_config.rs` |
| User content | An intro that calls everything untrusted, `>>> TRANSCRIPT START`…`END` (user, assistant, tool call and tool result entries, numbered), the session ID, then `>>> APPROVAL REQUEST START`, the retry reason (capped at 512 tokens), and the planned action as pretty JSON (`command`, `cwd`, `justification`, `sandbox_permissions`, `tool`, `tty`) | `CX/guardian-context/src/composition.rs`, `action.rs`, `CX/core/src/guardian/prompt.rs`, snapshot `guardian_review_request_layout.snap` |
| Trust | Only user and developer messages, AGENTS.md, and `request_user_input` answers are trusted; tool output, assistant text, and the action are untrusted evidence | `policy_template.md` |
| Budget | Per entry: 5K tokens for a message, 1K for a tool entry. In total: 20K for messages, 10K for tools, at most 40 recent non-user entries, at least 5 tool entries kept. Bytes / 4 estimates tokens. Later reviews in a session send only the transcript delta | `CX/guardian-context/src/profile.rs` |
| Answer | `{"risk_level","user_authorization","outcome","rationale"}`, only `outcome` required. A missing `risk_level` becomes low on allow and high on deny. Prose around the JSON is tolerated by slicing from the first `{` to the last `}` | `CX/ext/guardian-reviewer/src/assessment.rs` |
| Deadline and retries | 90 s per review, up to 3 attempts inside it. Retried: a parse error, rate limits, overload, 5xx, dropped connections. Backoff 200 ms × 2ⁿ with ±10% jitter | `CX/ext/guardian-reviewer/src/lib.rs:40`, `retry.rs` |
| Failure | Fails closed. A timeout tells the model: "did not finish before its deadline. Do not assume the action is unsafe based on the timeout alone. You may retry once, or ask the user" | `CX/prompts/src/model_messages/guardian.rs` |
| Circuit breaker | Per turn: 3 denials in a row, or 10 in the last 50 reviews, interrupt the turn once. Only model denials count; a failed review resets the run of denials | `CX/ext/guardian-reviewer/src/circuit_breaker.rs`, `review.rs:113` |
| Cost | Not stated in the source. The fixed prompt is about 18 KB (policy template 9.7 KB, policy 8.3 KB), plus the sub-session's permission and environment messages and tool definitions, plus up to 30K transcript tokens on the first review, then deltas on a reused, cached session | — |

### What uah does

- **One call, no tools.** `review.Reviewer.Review` makes one `llmcall.Call` over the adapter the caller gives it, so every provider works. The reviewer cannot inspect the disk, so the template's investigation section says to judge from the context and lean conservative.
- **Prompt.** Codex's template, trimmed of tool use, MCP, and browser rules (`internal/review/prompts/`, Apache-2.0 notice kept), with Codex's `policy.md` (three mentions of read-only checks removed) and output contract. The golden is `internal/review/testdata/prompt.golden`.
- **Context.** Three framed sections in one user message: the user's messages (trusted), the recent tool calls with a short status and no output (untrusted), and the approval request: the sandbox denial, then the action JSON (`tool`, `command`, `cwd`, `sandbox_mode`, `sandbox_permissions`, `justification`, `rule`). Assistant text and tool output are left out, as Claude Code does, because injected text reaches the model that way.
- **Budget.** `DefaultLimits`: 8 KB per user message, 24 KB for all of them (the first message, then the newest that fit), 1 KB per tool call, 8 KB and 10 calls for all of them, 8 KB for the command, justification, and denial each. Cut text keeps its head and tail around Codex's `<truncated omitted_approx_tokens="N" />`; dropped entries leave `<omitted … />`.
- **Answer.** Codex's fields. Parsing is strict: one JSON object, unknown fields and values rejected, only a surrounding Markdown fence is removed. A bad answer is retried up to 3 times within the deadline; transport errors are retried by the runner's client.
- **Failure.** Any error, timeout, or unparsable answer is a deny with `Failed` set and the reason; a timeout uses Codex's wording. A cancelled context returns an error instead.
- **Breaker.** 3 denials in a row, or 10 in the last 50; failed reviews count. When open, `Review` returns `AskUser` without calling the model, and the approver asks the user (headless: deny). `Reset` closes it; the approver calls it at each new user turn.
- **Configuration.** `approvals_reviewer = "auto_review" | "user"` (default `auto_review`, S8) and `[review] model`, `effort`, `timeout`. Defaults: `codex-auto-review` on `openai-codex`, the session model elsewhere, `low`, `90s`. `app.Resolve` returns them as `Resolved.ApprovalsReviewer` and `Resolved.Review`.

### Request shape and cost

One Responses API request per review: a system message (the policy, about 16.7 KB), one user message (the context), `reasoning.effort = "low"`, no tools, and `prompt_cache_key = "uah-review-<session ID>"` when the caller gives a session ID. The system message is the same for every review, so the prefix caches.

| | Input tokens | Output tokens | Latency | When |
| --- | --- | --- | --- | --- |
| uah, measured by the probe below | 3,471 (a one-line user message and no tool calls) | 210, of which 146 reasoning | 5.7 s | Each escalation the rules do not decide |
| uah, budget ceiling | about 3.2K fixed + up to 6K user messages + 2K tool calls + 6K action ≈ 17K; typical 4–6K | 100–300 | 3–8 s | |
| Codex, first review | the fixed prompt (about 4.5K) plus its sub-session's other messages and tools, plus up to 30K transcript | not stated | not stated | Each escalation, network approval, MCP call, patch, and permission request under auto-review |
| Codex, later reviews | the delta since the last review, on a cached session | | | |

A uah review costs about one small agent step on the same subscription, and less than Codex's first review because the transcript is smaller and there is no sub-session scaffolding.

### Probe

`go test -tags probe -run TestProbe -v ./internal/review/` (`internal/review/probe_test.go`) calls the `openai-codex` provider through the runner's client, which reads the Codex sign-in itself. On 2026-09-24 the backend **accepted `codex-auto-review` from uah's client**: `allow`, risk `medium`, 5.7 s, 3,471 input tokens (0 cached on this first call), 210 output tokens (146 reasoning), for "go test ./..." escalated after "Run the tests." So the default stands; no fallback was needed.

### Open decisions (defaults taken)

1. **Failed reviews count toward the breaker.** Default: yes, so a reviewer that keeps failing hands over to the user after 3. Codex resets the count on a failed review.
2. **No read-only investigation.** Default: a single call without tools. Codex's reviewer may run read-only commands first (for example to check what `rm -rf` would delete). Giving uah's reviewer the runner's read-only tools would need an agent loop, not `llmcall`.
3. **No transcript delta or session reuse.** Default: each review sends its whole (small) context, and the fixed prefix is cached by the prompt cache key. Codex keeps a review session and sends deltas.
4. **Assistant text and tool output left out.** Default: out, as the research recommended. Codex includes them as untrusted evidence under its budget.
5. **Budget sizes.** Default: `DefaultLimits` above (about 8K context tokens at most). Codex allows 20K message and 10K tool tokens.
6. **API-key `openai` provider.** Default: the session model. Codex uses `gpt-5.6-luna` there.
7. **A configurable policy.** `Request.Policy` replaces the default policy, as Codex's `[auto_review] policy` does, but there is no configuration key yet. Default: Codex's policy.
8. **Project files set the reviewer.** Default: a trusted project's `.uagent/config.toml` may set `approvals_reviewer` and `[review]`, like every other key. Making them user-file-only would stop a repository from turning auto-review on.
9. **No structured output.** The runner's `llm.Request` has no response-format field, so the JSON is asked for in the prompt only; Codex passes `final_output_json_schema`. Default: prompt plus strict parsing and retries.
10. **Effort on models without reasoning.** Default: `low` is always sent. Codex sends `low` only when the model lists it; a provider that rejects the field needs `[review] effort` or a runner-side check.
11. **Timeout.** Default: Codex's 90 s. The research suggested 30 s; `[review] timeout` changes it.
