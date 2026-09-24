<!-- memoria:section id="overview" files="approval.go" -->
# Approvals

The approver decides how a command runs: in the sandbox, outside it, or not at all. It applies the command rules and the approval policy and, when a command needs approval, asks through an `Ask` function that the engine and the session build. This README describes the whole permission pipeline on the embedded engine, from the sandbox to the user.

<!-- memoria:export id="summary" -->
On the embedded engine, each command runs in the sandbox unless a rule or an approval says otherwise: a command rule can allow, forbid, or ask; the model can ask to run a command outside the sandbox; and an escalation goes to the auto-reviewer, then PermissionRequest hooks, then the user. The defaults are Codex's: workspace-write, on-request, and auto-review.
<!-- /memoria:export -->

The pipeline follows Codex (checked against rust-v0.156.1). The decisions are recorded in the [sandbox plan](../../docs/design/sandbox.md), and the keys are in the [configuration reference](../../docs/configuration.md#sandbox-and-approvals).

1. [The pipeline](#the-pipeline)
2. [Defaults](#defaults)
3. [The approver](#the-approver)
4. [Where the rules come from](#where-the-rules-come-from)
5. [Tests](#tests)
<!-- /memoria:section -->

<!-- memoria:section id="pipeline" files="approval.go" -->
## The pipeline

For each Bash call on the embedded engine:

1. **PreToolUse hooks** run first (`internal/engine/embedded/pretooluse.go`). A hook can deny the call or rewrite its arguments. See [hooks](../hooks/README.md).
2. **Rules.** `Approver.Decide` splits the command into its simple commands and checks the [command rules](../rules/README.md). `forbidden` denies with the rule's justification. `allow` runs the command outside the sandbox without asking. `prompt` needs approval.
3. **Sandbox.** A command that no rule matched and that does not ask for escalation runs in the [sandbox](../sandbox/README.md). If it fails in a way that looks like a sandbox denial, the model is told it can ask for escalation. uah never retries by itself.
4. **Escalation.** The model asks to run a command outside the sandbox with `sandbox_permissions: "require_escalated"` and a `justification`. That command needs approval. Without a sandbox on the system, every command that no rule allows needs approval.
5. **Policy.** With `approval_policy = "never"`, or with no one to ask (`uah run` without an auto-reviewer or a PermissionRequest hook), the command is denied with a reason for the model.
6. **Auto-review.** With `approvals_reviewer = "auto_review"`, the [auto-reviewer](../review/README.md) judges the action: allow runs it, deny refuses it with the reviewer's reason, and "ask the user" (after too many denials) passes it on. A failed review denies.
7. **PermissionRequest hooks** can answer "allow" or "deny" for the user (`internal/session/approvals.go`).
8. **The user.** The TUI shows Codex's three choices: "Yes, proceed", "Yes, and don't ask again for commands that start with `<prefix>`", and "No, and tell the agent what to do differently". The agent waits; an interrupt declines.

An approved escalation runs outside the sandbox, with network. An approved `prompt` rule on a command that did not ask for escalation runs in the sandbox. A denied command is not run, and the model gets the reason as the tool's error.

The same ask (steps 6 to 8) serves MCP tools whose `approval_mode` needs approval, and subagents: a subagent runs its own auto-review, then asks its parent's user, with `agent <nickname>:` in front of the reason.

The process engine has none of this: its `SHELL` sandboxes every command, and nothing can ask.
<!-- /memoria:section -->

<!-- memoria:section id="defaults" files="approval.go" -->
## Defaults

| Setting | uah default | Codex |
| --- | --- | --- |
| `sandbox_mode` | `workspace-write` | The same for trusted projects |
| `network_access` | false | The same |
| Protected paths | `.git`, `.uagent`, `.agents`, `.codex` | `.git`, `.agents`, `.codex` |
| `approval_policy` | `on-request` (`on-failure` is accepted as `on-request`) | The same |
| `approvals_reviewer` | `auto_review` | Opt-in `auto_review` |
| Review model | `codex-auto-review` on openai-codex, else the session's model; low effort; 90 s timeout | `codex-auto-review` |
| Circuit breaker | 3 denials in a row, or 10 in the last 50 reviews, until the next user message | The same, except that a failed review counts as a denial in uah |
| Environment | The whole environment | The same |
<!-- /memoria:section -->

<!-- memoria:section id="approver" files="approval.go prefix.go" -->
## The approver

`Approver.Decide(ctx, Request, Ask) Decision` is the whole contract. It is safe for concurrent use, because the runner runs tools in parallel.

| Type | Carries |
| --- | --- |
| `Request` | The command, the working directory, whether the model asked for escalation, its justification and suggested `prefix_rule`, and whether no sandbox is available |
| `Decision` | `Sandboxed`, `Unsandboxed`, or `Deny`, with the reason the model hears |
| `Prompt` | What the user is asked: the command, the justification, whether it is an escalation, and the proposed prefix |
| `Answer` | `Approve`, `ApprovePrefix`, `Decline`, or `DeclineBecause(reason)`, which carries a reason such as the auto-reviewer's |
| `Ask` | `func(ctx, Prompt) Answer`. Nil means no one can answer |

"Don't ask again" is offered only when no rule matched. The prefix is the model's suggestion when it covers every simple command, else the whole command when it is one simple command, and never a bare shell, interpreter, `git`, `rm`, `sudo`, or `env` (Codex's list, in `prefix.go`). Choosing it appends `prefix_rule(pattern=[...], decision="allow")` to `~/.config/uagent/rules/default.rules` and applies it at once, also when the file cannot be written.

`Ask` is built in layers: the embedded engine wraps the session's ask with the auto-reviewer (`internal/engine/embedded/autoreview.go`), and the session's ask tries PermissionRequest hooks, then the user (`internal/session/approvals.go`).
<!-- /memoria:section -->

<!-- memoria:section id="sources" files="approval.go" -->
## Where the rules come from

`internal/app/approvals.go` builds the approver:

1. Every `*.rules` file in `~/.config/uagent/rules`, then, for a trusted workspace, in `<workspace>/.uagent/rules`.
2. `[approvals] allow` and `forbid` from the configuration, as prefix rules with `allow` and `forbidden` decisions. A trusted project file adds to the user file's lists.
3. The policy from `--ask`, `UAH_ASK`, or `approval_policy`.
<!-- /memoria:section -->

<!-- memoria:section id="tests" files="approval_test.go" -->
## Tests

`approval_test.go` pins the decision table: each rule decision, escalation with and without a sandbox, the policies, headless denial, and "don't ask again". `internal/engine/embedded/approval_test.go` runs the pipeline end to end on the embedded engine with `testing/fakellm`, including PermissionRequest hooks and auto-review.
<!-- /memoria:section -->
