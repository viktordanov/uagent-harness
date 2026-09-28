# Shell mode (`!`)

Ledger item 38: type `!` at the start of an empty composer, and enter runs the line as a command in the workspace instead of sending it to the agent. The command and its output join the conversation, so the agent sees them on its next turn.

1. [What Claude Code does](#what-claude-code-does)
2. [What Codex does](#what-codex-does)
3. [The design](#the-design)
4. [Decisions](#decisions)
5. [Open](#open)

## What Claude Code does

Checked against the Claude Code documentation on 2026-09-24 (the pages name v2.1.275 as the newest version they describe).

- **The prompt.** `!` at the start of the input enters shell mode ([interactive mode, "Shell mode with `!` prefix"](https://code.claude.com/docs/en/interactive-mode#shell-mode-with-prefix)). Escape, Backspace, or Ctrl+U on an empty prompt leaves it. Pasting text that starts with `!` into an empty prompt enters it too. Tab completes from earlier `!` commands and file paths.
- **The conversation.** Shell mode "adds the command and its output to the conversation context" and "shows real-time progress and output". Since v2.1.186 Claude responds to the output at once, which costs a normal prompt; `respondToBashCommands = false` adds it to the context without a reply ([settings reference, `respondToBashCommands`](https://code.claude.com/docs/en/settings-reference#respondtobashcommands)). The documentation does not give the text the model sees.
- **Limits.** Shell mode names no limit of its own. The Bash tool's output limits are about 30,000 characters inline for a good result and 10,000 for a failure (`BASH_MAX_OUTPUT_LENGTH`, [tools reference, "Output limits"](https://code.claude.com/docs/en/tools-reference#output-limits)), and its default timeout is 2 minutes (`BASH_DEFAULT_TIMEOUT_MS`, [environment variables](https://code.claude.com/docs/en/env-vars)). Ctrl+B sends a long `!` command to the background.
- **While Claude works.** A `!` command queues like a message and runs when the turn ends ([interactive mode, "Queue messages while Claude works"](https://code.claude.com/docs/en/interactive-mode#queue-messages-while-claude-works)). Ctrl+Enter in shell mode queues it without interrupting the turn.
- **Sandbox and approval.** The command "doesn't require Claude to interpret or approve the command". It runs outside the sandbox even with sandboxing on, "because the sandbox applies to the commands Claude runs". Only background sessions and Linux sessions with `CLAUDE_CODE_SUBPROCESS_ENV_SCRUB` sandbox it ([sandboxing, "The unsandboxed retry escape hatch"](https://code.claude.com/docs/en/sandboxing#the-unsandboxed-retry-escape-hatch)). Before v2.1.260, strict sandbox mode sandboxed shell-mode commands in every session.

## What Codex does

Checked against Codex rust-v0.156.1. Paths are under `codex-rs/`.

- **How it runs.** The TUI sends `Op::RunUserShellCommand { command, timeout_ms }` (`protocol/src/protocol.rs:755-767`), through the app server's `thread/shellCommand`. `core/src/tasks/user_shell.rs` runs it:
  - It uses the user's shell as a login shell: `[shell, "-lc", command]` (`user_shell.rs:142-146`, `core/src/shell.rs:22-49`).
  - It runs without a sandbox: `PermissionProfile::Disabled` and `SandboxType::None` (`user_shell.rs:212, 225`). The app server's documentation says it "runs unsandboxed with full access rather than inheriting the thread sandbox policy" (`app-server-protocol/src/protocol/v2/thread.rs:1160-1173`).
  - It gets no network proxy: "`/shell` is the explicit full-access escape hatch" (`user_shell.rs:219-220`).
  - It runs in the turn's working directory (`user_shell.rs:149`).
  - Its environment comes from the same `shell_environment_policy` as the agent's commands (`user_shell.rs:168-171`).
  - It stops after `USER_SHELL_TIMEOUT_MS`, one hour (`user_shell.rs:48, 223`).
  - No exec policy or approval step applies: the command goes straight to `execute_exec_request` (`user_shell.rs:244`).
- **What the model sees.** A user-role message, the `UserShellCommand` context fragment (`core/src/context/user_shell_command.rs:30-52`). Codex's test shows it (`core/src/user_shell_command_tests.rs:38`):

  ```text
  <user_shell_command>
  <command>
  echo hi
  </command>
  <result>
  Exit code: 0
  Duration: 1.0000 seconds
  Output:
  hi
  </result>
  </user_shell_command>
  ```

  - The output is stdout and stderr interleaved. A timeout puts `command timed out after <ms> milliseconds` before it (`core/src/tools/mod.rs:125-145`).
  - The output is cut to the model's truncation policy, 10,000 tokens (about 40,000 bytes), keeping the head and the tail around a marker (`utils/output-truncation/src/lib.rs:20-31`, `utils/string/src/truncate.rs`).
  - A failure is recorded too. A command that does not start records exit code -1 with `execution error: …`. A canceled one records -1 with `command aborted by user` (`user_shell.rs:249-259, 325, 366`).
- **Turns.** The command never samples the model. With no turn running, it is a turn of its own that ends without a model call (`user_shell.rs:84-101`). Its record is written to the history and the rollout, and the model sees it with the next user turn (`user_shell.rs:453-481`).
- **During a turn.** It runs at once, concurrently, with the turn's cancellation token, so an interrupt kills it too (`core/src/session/handlers.rs:101-117`). Its record is injected into the running turn's pending input (`inject_no_new_turn`, `core/src/session/inject.rs:170-188`), and the model sees it at its next sampling step.
- **The TUI.**
  - `!` as the first character switches the composer to shell mode (`tui/src/bottom_pane/chat_composer.rs:3760-3768`). The prompt becomes a light red bold `!` (`:4915-4917`), and the footer says "Shell mode" (`:3570-3577`).
  - Esc on an empty composer leaves shell mode (`:3465-3472`), and so does Backspace at the start (`:3719-3729`). The slash popup is off in shell mode.
  - The command shows as an exec cell titled "You ran", with up to 50 output rows cut in the middle (`tui/src/exec_cell/render.rs:42-43, 466-474, 573-620`). Its output streams in as it arrives.

## The design

- **Composer (`internal/tui/state/shell.go`).**
  - `!` on an empty composer sends `EnterShell`, and the reducer sets `State.Shell`. Backspace on the empty composer (`LeaveShell`) or esc leaves.
  - In shell mode, enter and ctrl+enter give `EffShell{Command}`, and the composer returns to messages. A draft that starts with `/` is a path, not a command, and the menu stays closed.
  - `render.ShellPrompt` and `render.ShellPlaceholder` draw the `!` and the hint. The footer's hint becomes `! shell mode · enter runs the command · esc leaves`. The bubble shell only maps keys and applies the prompt after each reduce.
- **Running (`internal/usershell`, `Session.RunShell`).**
  - The session owns the command. `RunShell(ctx, command)` runs it at once in the workspace, as `<user shell> -c <command>`, which is how the agent's Bash commands run.
  - The shell comes from `sandbox.Shell`, so the environment policy applies as it does for the agent.
  - The command stops after an hour (Codex's limit), when its context ends, on the session's interrupt (esc esc, `/stop`), or when the session closes.
  - The output streams as `ShellOutput` events. `ShellStarted` and `ShellFinished` bracket it.
  - `internal/app` builds the runner, so both engines have it: on the process engine the command runs in uah, not in the runner.
- **The conversation.**
  - The record is Codex's `<user_shell_command>` text, byte for byte (`usershell.Record.Text`).
  - Stdout and stderr are interleaved, and the output is cut to 40,000 characters: the runner's Bash default (`operation.DefaultMaxOutputLength`), close to Codex's 10,000 tokens.
  - The cut keeps the first and last half around the Bash tool's `...N bytes truncated...` marker. Memory holds at most 160 KB of each end.
  - A failed, stopped, or refused command is recorded too, with Codex's exit code -1 and its `execution error:`, `command aborted by user`, and `command timed out after` texts.
  - The record goes to the agent the way `Session.Inject` does: held, and sent before the next run's messages with the command's ID. It never starts a run.
- **Transcript and resume.**
  - The command is a `KindShell` item keyed by its ID. It is drawn on the band after an accent `!`, with its status (`✓`, `✗ exit N`, `■ stopped`, `✗ not run`, or a spinner) and its output folded: 10 rows in the compact view and 50 in the detailed view, the first and last half around `… N more lines`.
  - Until the agent has the record, a dim line says "the agent sees this with your next message".
  - The runner's echo of the record carries the same ID and updates the item in place. A resumed transcript parses the saved message (`usershell.Parse`) and shows it as the command, not as tagged text.

## Decisions

- **No sandbox and no rules by default, as in Codex.** Codex runs the user's command with full access and no exec policy. Claude Code runs it outside the sandbox and without approval. Typing a command is the user acting in their own terminal: `! git commit` must work, and the workspace sandbox protects `.git`.
  - `user_shell_sandbox = true` runs the command like the agent's instead, which is what the ledger item first described. A `forbidden` rule refuses it before it runs, with the rule's reason as its output. An `allow` rule runs it outside the sandbox. Anything else runs in the sandbox of the session's current permission mode.
  - A `prompt` rule or `approval_policy = "never"` does not ask again, because typing the command was the approval (`approval.Approver.DecideTyped`). This follows Claude Code's strict sandbox mode for background sessions.
  - Without a sandbox on the system, the command runs without one, as the process engine runs the agent's.
- **With the next message, never a turn of its own.** This follows Codex, whose command never calls the model. Claude Code since v2.1.186 replies to the output unless `respondToBashCommands = false`. uah keeps the cheaper behavior, and the user asks about the output in the next message.
- **At once during a run, not injected into it.** Codex runs the command concurrently and adds its record to the running turn. uah runs it at once too, but holds the record for the next run, as `Session.Inject` holds a subagent's notification: sending a message into a live run makes the runner cancel its model request, which would throw away a paid request. The interrupt that stops the agent also stops the user's running commands, as Codex's cancellation token does.
- **`-c`, not a login shell.** Codex runs `-lc`. uah runs `-c`, as the agent's Bash commands do, so the user's command sees the environment the agent's commands see, and a login profile cannot print into the output.

## Open

Defaults taken; the owner may change them.

1. **A record lost on quit.** The record is held in memory until the next message. Quitting before that loses it, where Codex writes it to the rollout at once. Keeping it in the sidecar would need the sidecar to hold messages.
2. **No background, no completion.** Claude Code's Ctrl+B and its `!` history and path completion are not built.
3. **Paste.** Text pasted into an empty composer that starts with `!` stays a message; Claude Code enters shell mode for it. Codex queues it as a literal message.
