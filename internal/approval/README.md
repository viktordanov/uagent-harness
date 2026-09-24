<!-- memoria:section id="overview" files="approval.go mode.go" -->
# Approvals

The approver decides how a command runs: in the sandbox, outside it, or not at all. It applies the command rules and the approval policy and, when a command needs approval, asks through an `Ask` function that the engine and the session build. This README describes the whole permission pipeline on the embedded engine, from the sandbox to the user.

<!-- memoria:export id="summary" -->
On the embedded engine, each command runs in the sandbox unless a rule or an approval says otherwise: a command rule can allow, forbid, or ask; the model can ask to run a command outside the sandbox; and an escalation goes to PermissionRequest hooks, then you. The permission mode, which shift+tab cycles, picks the sandbox and who answers: you in read-only and workspace, the auto-reviewer alone in auto. The defaults are Codex's: workspace-write, on-request, and the user as reviewer (Codex's "Ask for approval").
<!-- /memoria:export -->

The pipeline follows Codex (checked against rust-v0.156.1). The decisions are recorded in the [sandbox plan](../../docs/design/sandbox.md), and the keys are in the [configuration reference](../../docs/configuration.md#sandbox-and-approvals).

1. [The pipeline](#the-pipeline)
2. [Patches](#patches)
3. [Permission modes](#permission-modes)
4. [Defaults](#defaults)
5. [The approver](#the-approver)
6. [Where the rules come from](#where-the-rules-come-from)
7. [Tests](#tests)
<!-- /memoria:section -->

<!-- memoria:section id="pipeline" files="approval.go" -->
## The pipeline

For each Bash call on the embedded engine:

1. **PreToolUse hooks** run first (`internal/engine/embedded/pretooluse.go`). A hook can deny the call or rewrite its arguments. See [hooks](../hooks/README.md).
2. **Rules.** `Approver.Decide` splits the command into its simple commands and checks the [command rules](../rules/README.md). `forbidden` denies with the rule's justification. `allow` runs the command outside the sandbox without asking. `prompt` needs approval.
3. **Sandbox.** A command that no rule matched and that does not ask for escalation runs in the [sandbox](../sandbox/README.md) of the current [permission mode](#permission-modes). If it fails in a way that looks like a sandbox denial, the model is told it can ask for escalation. uah never retries by itself.
4. **Escalation.** The model asks to run a command outside the sandbox with `sandbox_permissions: "require_escalated"` and a `justification`. That command needs approval. Without a sandbox on the system, every command that no rule allows needs approval.
5. **Policy.** With `approval_policy = "never"`, or with no one to ask (`uah run` without an auto-reviewer or a PermissionRequest hook), the command is denied with a reason for the model.
6. **Auto-review.** With `approvals_reviewer = "auto_review"`, the [auto-reviewer](../review/README.md) judges the action: allow runs it, deny refuses it with the reviewer's reason, and "ask the user" (after too many denials) passes it on. A failed review denies. In Auto mode the reviewer judges whatever `approvals_reviewer` says, and it decides alone: "ask the user" is a decline with its reason, and steps 7 and 8 never run.
7. **PermissionRequest hooks** can answer "allow" or "deny" for the user (`internal/session/approvals.go`).
8. **The user.** The TUI shows Codex's three choices: "Yes, proceed", "Yes, and don't ask again for commands that start with `<prefix>`", and "No, and tell the agent what to do differently". For an MCP tool, the middle choice is "Yes, and don't ask again for this tool" (`a`), which sets the tool's `approval_mode` to `approve` in the file that configures its server and in the running session ([MCP approvals](../mcp/README.md#approvals)). The agent waits; an interrupt declines.

An approved escalation runs outside the sandbox, with network. An approved `prompt` rule on a command that did not ask for escalation runs in the sandbox. A denied command is not run, and the model gets the reason as the tool's error.

The same ask (steps 6 to 8) serves [patches](#patches) that write outside the sandbox, MCP tools whose `approval_mode` needs approval, and subagents: a subagent runs its own auto-review, then asks its parent's user, with `agent <nickname>:` in front of the reason. A subagent whose role has an `approve` list answers those prompts itself, before its auto-review; an escalation in read only mode is still asked ([Markdown agents](../agents/README.md#markdown-agents)).

The process engine applies steps 2, 3, and 5 in the shell it gives the runner, with the same `Approver.Decide` and no one to ask ([the shell gate](../engine/README.md#the-process-engine)): `forbidden` refuses, `allow` runs outside the sandbox, `prompt` refuses with the headless reason, and the rest runs in the sandbox. It has no escalation, no auto-review, and no PermissionRequest hooks.
<!-- /memoria:section -->

<!-- memoria:section id="patches" files="approval.go" -->
## Patches

An `apply_patch` call (see [patches](../patch/README.md)) goes through the same pipeline, as Codex's patch approval does (`assess_patch_safety` in `codex-rs/core/src/safety.rs`). The embedded engine checks each path the patch writes, move destinations included, against the sandbox policy of the run's current permission mode (`sandbox.Policy.CanWrite`):

| Mode | A write inside the writable roots | Any other write |
| --- | --- | --- |
| Read only | Asks | Asks |
| Workspace | Applies | Asks |
| Auto | Applies | The auto-reviewer decides |
| Full access | Applies | Applies |

A protected path (`.git`, `.uagent`, `.agents`, `.codex`) is not inside the writable roots, and a symlink is followed before the check. A patch that needs approval becomes a `Request` with `Command` `apply_patch <paths>` (so a rule on the prefix `apply_patch` allows or forbids such patches), `Escalated`, the reason (`the patch writes outside the writable roots`, or `the sandbox is read-only`), and `Tool` and `Input` set. PermissionRequest hooks then see `tool_name` `apply_patch` with Codex's `{"command": "<patch>"}`, and the auto-reviewer sees the patch. A decline, or no one to ask, is the tool's error, and nothing is written. A patch that cannot apply fails before anyone is asked.
<!-- /memoria:section -->

<!-- memoria:section id="modes" files="mode.go" -->
## Permission modes

A permission mode (`Mode`) is a sandbox mode and who decides what needs approval. shift+tab in the TUI cycles read only, workspace, and auto; `permission_mode` in the configuration, or `--sandbox`, picks one at start. The session keeps it (`session.Settings.Mode`), saves it in its sidecar, and sends it to the engine with each run (`engine.Options.Mode`) and to a live run (`Run.SetMode`).

| Mode | Sandbox | Escalations and `prompt` rules | Codex | Claude Code |
| --- | --- | --- | --- | --- |
| `read-only` | `read-only` | Ask: PermissionRequest hooks, then you (the auto-reviewer first only with `approvals_reviewer = "auto_review"`) | `read-only` preset ("Read Only"), on-request | `plan` is the nearest: it reads and does not change files |
| `workspace` (default) | `workspace-write` | Ask: PermissionRequest hooks, then you (the auto-reviewer first only with `approvals_reviewer = "auto_review"`) | `auto` preset ("Default") with `approvals_reviewer = user` ("Ask for approval") | `default` |
| `auto` | `workspace-write` | The auto-reviewer decides; the user is not asked. A decline reaches the model with the reviewer's reason | `auto` preset with `approvals_reviewer = auto_review` ("Approve for me") | `auto`: a classifier model approves or blocks each action, and a block goes back to Claude with the reason |
| `full-access` | none | No escalations; `prompt` rules ask | `full-access` preset ("Full Access"), approval never | `bypassPermissions` |

What uah takes from each:

- **Codex (rust-v0.156.1).** The presets pair an approval policy with a sandbox: `read-only`, `auto`, and `full-access` (`codex-rs/utils/approval-presets/src/lib.rs:28-61`). Its permission shortcut cycles only `read-only`, `auto` with the user as reviewer, and `auto` with the auto-reviewer, and leaves Full Access out (`codex-rs/tui/src/chatwidget/permission_shortcuts.rs:36-99`; the labels are in `codex-rs/tui/src/chatwidget.rs:516-517`). It applies a choice to the live thread as a turn-context override of the approval policy, the reviewer, and the permission profile, from the next turn (`codex-rs/tui/src/app/thread_settings.rs:147-186`). Codex binds that shortcut to no key by default (`codex-rs/tui/src/keymap.rs:1671-1672`) and keeps shift+tab for its collaboration mode (`keymap.rs:2466-2467`).
- **Claude Code** ([permission modes](https://code.claude.com/docs/en/permission-modes)). shift+tab cycles `default`, `acceptEdits`, and `plan`, then `bypassPermissions` when the session started with it allowed, then `auto` when auto mode is available. In auto mode a separate classifier model reviews each action instead of the user, and a blocked action goes back to Claude with the reason; after 3 blocks in a row or 20 in total it pauses and asks the user again. The status bar names the mode (`⏵⏵ auto mode on`). uah takes the key, the cycle, the mode in the footer, and auto mode's judge, with Codex's auto-reviewer as the judge. It does not take `acceptEdits`, because uah's agent edits files through commands, which the workspace sandbox already allows.

Differences: Full Access keeps `approval_policy` as configured (Codex's preset sets `never`), because uah's `approval_policy` is a separate key. Auto mode forces the auto-reviewer on, also with `approvals_reviewer = "user"`. Once its circuit breaker opens (3 denials in a row, or 10 in the last 50 reviews), uah's Auto mode declines with the reason, where Claude Code's auto mode goes back to asking the user and Codex's workspace mode asks too; the user is never asked in Auto mode, and switching to Workspace mode brings the prompts back. `approval_policy = "never"` denies what needs approval in every mode.

A change reaches a live run on the embedded engine from its next command and model request: the Bash tool picks that mode's sandboxing shell for each command, and each model request describes that sandbox to the model. On the process engine it applies from the next run, and the TUI says so. A subagent starts in its parent's mode at the time it spawns.
<!-- /memoria:section -->

<!-- memoria:section id="defaults" files="approval.go" -->
## Defaults

| Setting | uah default | Codex |
| --- | --- | --- |
| `permission_mode` | `workspace` (workspace-write, ask) | The `auto` preset ("Default") |
| `sandbox_mode` | `workspace-write` | The same for trusted projects |
| `network_access` | false | The same |
| Protected paths | `.git`, `.uagent`, `.agents`, `.codex` | `.git`, `.agents`, `.codex` |
| `approval_policy` | `on-request` (`on-failure` is accepted as `on-request`) | The same |
| `approvals_reviewer` | `user` (auto mode uses the reviewer) | `user`; `auto_review` with "Approve for me" |
| Review model | `codex-auto-review` on openai-codex, else the session's model; low effort; 90 s timeout | `codex-auto-review` |
| Circuit breaker | 3 denials in a row, or 10 in the last 50 reviews, until the next user message | The same, except that a failed review counts as a denial in uah |
| Environment | The whole environment | The same |
<!-- /memoria:section -->

<!-- memoria:section id="approver" files="approval.go prefix.go typed.go" -->
## The approver

`Approver.Decide(ctx, Request, Ask) Decision` is the whole contract. It is safe for concurrent use, because the runner runs tools in parallel.

| Type | Carries |
| --- | --- |
| `Request` | The command, the working directory, whether the model asked for escalation, its justification and suggested `prefix_rule`, and whether no sandbox is available |
| `Decision` | `Sandboxed`, `Unsandboxed`, or `Deny`, with the reason the model hears |
| `Prompt` | What the user is asked: the command, the justification, whether it is an escalation, the proposed prefix, and, for an MCP call, the tool's qualified name (`MCPTool`) |
| `Answer` | `Approve`, `ApprovePrefix`, `ApproveTool` (an MCP tool, from now on), `Decline`, or `DeclineBecause(reason)`, which carries a reason such as the auto-reviewer's |
| `Ask` | `func(ctx, Prompt) Answer`. Nil means no one can answer |

"Don't ask again" is offered only when no rule matched. The prefix is the model's suggestion when it covers every simple command, else the whole command when it is one simple command, and never a bare shell, interpreter, `git`, `rm`, `sudo`, or `env` (Codex's list, in `prefix.go`). Choosing it appends `prefix_rule(pattern=[...], decision="allow")` to `~/.config/uagent/rules/default.rules` and applies it at once, also when the file cannot be written.

`DecideTyped(command)` decides a command the user typed in the TUI's shell mode when `user_shell_sandbox = true`: a `forbidden` rule refuses it and an `allow` rule runs it outside the sandbox, as for the agent; anything else runs in the sandbox without asking, since typing it was the approval. By default the user's commands skip the rules and the sandbox, as in Codex ([shell mode](../../docs/design/shell-mode.md)).

`Ask` is built in layers: the embedded engine wraps the session's ask with the auto-reviewer (`internal/engine/embedded/autoreview.go`), and the session's ask tries PermissionRequest hooks, then the user (`internal/session/approvals.go`).
<!-- /memoria:section -->

<!-- memoria:section id="sources" files="approval.go" -->
## Where the rules come from

`internal/app/approvals.go` builds the approver:

1. Every `*.rules` file in `~/.config/uagent/rules`, then, for a trusted workspace, in `<workspace>/.uagent/rules`.
2. `[approvals] allow` and `forbid` from the configuration, as prefix rules with `allow` and `forbidden` decisions. A trusted project file adds to the user file's lists.
3. The policy from `--ask`, `UAH_ASK`, or `approval_policy`.
<!-- /memoria:section -->

<!-- memoria:section id="tests" files="approval_test.go mode_test.go typed_test.go" -->
## Tests

`approval_test.go` pins the decision table: each rule decision, escalation with and without a sandbox, the policies, headless denial, and "don't ask again". `mode_test.go` pins the modes' sandboxes, who decides, and the cycle. `typed_test.go` pins `DecideTyped`. `internal/engine/embedded/approval_test.go` runs the pipeline end to end on the embedded engine with `testing/fakellm`, including PermissionRequest hooks and auto-review, and `internal/engine/embedded/mode_test.go` the modes: a live switch to read only, and Auto mode deciding without the user.
<!-- /memoria:section -->
