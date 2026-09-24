# Sandboxing and approvals: plan

Status: decided 2026-09-24; phases 1 and 2 built (see [As built](#as-built-phase-1)). The research and the options are in [sandbox-research.md](sandbox-research.md); Codex facts are from openai/codex at rust-v0.156.1.

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
| `internal/approval/review.go` | The reviewer: user messages (trusted), the last tool calls without their output (untrusted), the action, and a fixed policy prompt. JSON out: outcome, risk, reason |
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

## As built (phase 2)

- **Packages.** `internal/rules` parses `.rules` files with `go.starlark.net` (`prefix_rule`; `host_executable` and `network_rule` load but do nothing) and splits commands with `mvdan.cc/sh/v3/syntax`: plain words and quotes joined by `&&`, `||`, `;`, and `|`. Anything else matches no rule, so the default policy applies, as with Codex's tree-sitter parser. An absolute program path also matches its base name. `internal/approval` holds the approver: `Decide(ctx, Request, Ask) Decision`, where `Request` has the command, cwd, escalation flag, justification, and the model's `prefix_rule`, and `Decision` is deny (with the reason for the model), sandboxed, or unsandboxed.
- **Order in `Translate`.** PreToolUse hooks, then rules (`forbidden` denies with the justification; `allow` runs unsandboxed without asking, also headless), then the policy: an escalation or a `prompt` rule asks, unless the policy is `never` or no one can answer (`uah run`), which deny with a reason. An approved `prompt` rule without escalation runs sandboxed, as in Codex. The unsandboxed shell is a second runner Bash translator whose shell is the real one (with the environment policy); the sandbox hint is added only to commands that ran in the sandbox.
- **Asking.** `engine.Options.Ask` carries the session's asker into each run, so the engine never imports the session. The session emits `ApprovalRequested` and waits for `Session.Resolve(id, answer)`; pending approvals live in a loop-owned map. An interrupt, the run's end, `Close`, or the run's context declines them (`ApprovalResolved` with `decline`). `session.Options.Interactive` is set by the TUI only.
- **"Don't ask again".** It proposes the model's `prefix_rule` when it covers every command and is not on Codex's banned list, else the whole command when it is one simple command, else nothing. The chosen prefix is appended to `~/.config/uagent/rules/default.rules` and applies at once; if the file cannot be written, it still applies for the session and the error is logged.
- **Without a sandbox** on the embedded engine (Linux without bwrap), each command asks unless a rule allows it (S12). `danger-full-access` offers no escalation, but rules still apply.
- **Configuration.** `approval_policy` (and `--ask`, `UAH_ASK`); `[approvals] allow` and `forbid` are command prefixes that become rules. A trusted project may set the policy and adds prefixes, rules files, and writable roots.
- **TUI.** The overlay replaces the queue panel: "Run outside the sandbox?" (or "Run this command?" for a `prompt` rule), the reason, `$ command`, and `y`, `s`, `n`/esc. The answer is recorded as a transcript line.

### Open (defaults taken)

- "No, and tell the agent what to do differently" declines with a fixed reason; the user types the instruction as the next message. Codex opens a text field.
- There is no session cache of approved commands ("don't ask again for this command in this session"); only prefix rules persist.
- `[approvals]` has no `prompt` list; a `prompt` rule needs a `.rules` file.
- A project's `[approvals] allow` and `.rules` `allow` rules run commands unsandboxed; they apply only to trusted projects, as hooks do, but need no separate trust step.

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
