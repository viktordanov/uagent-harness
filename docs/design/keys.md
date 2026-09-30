# Send keys: send before the next model request, or queue

While the agent works, uah has two ways to send a message, as Codex rust-v0.159.1 has. Enter gives it to the agent after its running tool calls, before its next model request. Tab queues it for the end of the run. The keys are the same in every terminal, so they also work where ctrl+enter cannot be told from enter. The terminal's answer to the keyboard enhancement query only picks the new-line hint.

1. [The problem](#the-problem)
2. [What Codex and Claude Code do](#what-codex-and-claude-code-do)
3. [What the terminal reports](#what-the-terminal-reports)
4. [The bindings](#the-bindings)
5. [Decisions](#decisions)
6. [Tests](#tests)

## The problem

Before this change, enter queued while the agent worked, ctrl+enter or alt+enter sent now, and shift+enter or ctrl+j added a line. tmux has `extended-keys off` by default. With that setting, ctrl+enter reaches the program as plain enter, so "send now" quietly became "queue", and nothing happened until the run ended. The owner runs uah inside a tmux-backed browser terminal, where this happens every time. The owner also decided that a message sent while the agent works should go after the next tool call, not after the whole run, which is Codex's enter.

A probe with Bubble Tea v2.0.9 inside tmux 3.7c (`tmux -L probe -f /dev/null`, `send-keys C-Enter`, and so on) reported these keys:

| Sent | `extended-keys off` | `on`, `always` (either `extended-keys-format`) |
| --- | --- | --- |
| C-Enter | `enter` | `ctrl+enter` |
| S-Enter | `enter` | `shift+enter` |
| M-Enter | `alt+enter` | `alt+enter` |
| C-j | `ctrl+j` | `ctrl+j` |
| Tab | `tab` | `tab` |

## What Codex and Claude Code do

**Codex rust-v0.159.1** (`codex-rs/tui`):

- **Bindings.** Enter submits and tab queues, in every terminal. The keymap defaults are `submit: plain(Enter)` and `queue: plain(Tab)` (`keymap.rs`). While a task runs, enter's `InputResult::Submitted` goes into the live turn as a pending steer (`chatwidget/input_flow.rs`). Tab returns `InputResult::Queued`, which waits for the turn's end (`chat_composer.rs`). The steer feature flag is gone: `set_steer_enabled` is a no-op kept for tests.
- **No flush key.** Codex has no key that sends the queue now. The queue goes out when the turn ends.
- **Keyboard enhancement.** Codex always pushes the kitty flags and probes with `CSI ? u`, waiting up to 250 ms (`terminal_probe.rs`). The answer changes only the footer's newline hint: shift+enter where the terminal reports enhancements, ctrl+j elsewhere (`footer_insert_newline_key`). Steer and queue never depend on it.
- **Footer hint.** While a task runs, the footer says "tab to queue message".

**Claude Code** (docs at code.claude.com, interactive mode, read 2026-09-30):

- **Enter.** Enter while Claude works queues the message. A message queued during tool calls reaches Claude when those calls finish, within the same turn. What is left goes out in order at the turn's end.
- **Send now.** Ctrl+Enter, or the chord Ctrl+X Ctrl+S, sends the queued messages now. The docs say that in terminals without extended keys, Ctrl+Enter arrives as Enter and queues. The chord works everywhere. Claude Code does not detect this case.
- **New line.** `\` + Enter, Option+Enter, Shift+Enter (native in several terminals), and Ctrl+J, which works in any terminal.

Details beyond the bindings, from the same Codex version:

- **The steer's preview.** A message sent with enter while the turn runs shows under "Messages to be submitted after next tool call (press Esc to interrupt and send immediately)" (`pending_input_preview.rs`). If the core rejects the steer, it moves to "Messages to be submitted at end of turn".
- **Tab while idle.** Tab submits like enter, "so input is never dropped", except for a `!` command (`chat_composer.rs` module comment).
- **Other keys.** Esc with pending steers interrupts and sends them. Shift+Left or Alt+Up edits the last queued message. Codex also pushes the kitty flags with care inside tmux: it reads `extended-keys-format` with `tmux display-message` and leaves out event types unless the format is `csi-u` (`tui/tmux.rs`, `keyboard_modes.rs`).

Claude Code's docs also give the tmux settings that make shift+enter work: `set -s extended-keys on`, `set -as terminal-features 'xterm*:extkeys'`, and `allow-passthrough on`.

## What the terminal reports

Bubble Tea v2.0.9 always asks for key disambiguation. On the first frame, and on each change of `View.KeyboardEnhancements` or the alt screen, it writes modifyOtherKeys (`CSI > 4 ; 2 m`), pushes the kitty flags, and queries them with `CSI ? u`. A terminal that speaks the kitty keyboard protocol answers, and the program gets `tea.KeyboardEnhancementsMsg`, where `SupportsKeyDisambiguation()` is true for flags above 0. A terminal that does not answer sends nothing. Bubble Tea has no timeout and no "not supported" message.

| Terminal | The message | ctrl+enter arrives as | Source |
| --- | --- | --- | --- |
| tmux 3.7c, `extended-keys off` | none | `enter` | measured |
| tmux 3.7c, `extended-keys on` or `always` | none | `ctrl+enter` (modifyOtherKeys) | measured |
| A pty that answers `CSI ? 1 u`, as kitty does | `{Flags:1}` | `ctrl+enter` | measured with the probe under a Python pty |
| kitty, Ghostty, WezTerm, foot | expected, since they implement the kitty protocol | `ctrl+enter` | their documentation, not measured here |
| Terminal.app | none expected: no kitty protocol | `enter` | not measured here |
| iTerm2 | depends on its CSI u setting | varies | not measured here |

tmux does not answer the query in any mode. With extended keys on, ctrl+enter arrives, but the message does not, so tmux alone cannot tell uah whether the keys work. Codex's probe gives the same result: to Codex, tmux is a terminal without enhancements. For this reason, uah's send keys do not depend on the answer.

## The bindings

| Key | While the agent works | While idle |
| --- | --- | --- |
| enter | Send now: the agent reads it after its running tool calls, before its next model request (`Steer`) | Send (`Submit`) |
| enter on an empty composer | Send the queued messages now, in order | The same, for a queue an interrupt kept; nothing without a queue |
| tab | Queue for the end of the run (`Submit`) | Send, as enter. In shell mode, on an empty composer, and in the open menu, it keeps its own meaning |
| ctrl+enter, alt+enter | As enter | As enter |
| shift+enter, ctrl+j | New line; shift+enter only where the terminal tells it from enter | The same |

The footer shows `enter send now · tab queue` while the agent works. In the detailed view while idle it shows `enter send · shift+enter new line` where the terminal answered the query, and `enter send · ctrl+j new line` elsewhere, as Codex's footer picks its new-line key. The queue's hint says `enter sends now`. `/help` gives the keys and the new-line key for this terminal.

`State.SendIntent(key, draft)` maps the keys, and `State.Working` says whether the agent the composer talks to works: in the agent view, the viewed subagent. `State.Keys.Disambiguated` is the terminal's answer, set by `state.KeyboardReported`, which the shell sends for `tea.KeyboardEnhancementsMsg`.

## Decisions

- **Codex's model, in every terminal.** The owner's decision: queued messages go after the next tool call, not after the whole run. Enter is Codex's steer and tab is Codex's queue. The keys no longer depend on the terminal, so tmux with `extended-keys off` needs no detection for sending.
- **Enter on an empty composer sends the queue now.** Codex has no key for this: its queue goes out at the turn's end, and esc sends pending steers after an interrupt. uah keeps the send-the-queue-now action it had on ctrl+enter, on the key that works in every terminal. ctrl+enter and alt+enter keep it too.
- **ctrl+enter and alt+enter are aliases of enter.** They are harmless where the terminal shows them, and in tmux they arrive as enter anyway.
- **The process engine.** It has no live input. Enter while the agent works does what steering did there before: the run stops and restarts with the queue and the message (`Session.dispatch`). The embedded engine, the default, takes the message into the live run.
- **The terminal's answer only picks the new-line hint.** It is what Codex uses it for. Where no answer comes (tmux, Terminal.app), shift+enter arrives as enter: the same bytes, so uah cannot tell the two apart, and it sends. The footer and `/help` name ctrl+j there, and `/help` says that shift+enter sends in this terminal. Before the answer arrives, the hint says ctrl+j, which works everywhere, so no timer is needed.
- **No warning, no doctor check, and no setting.** The first version of this item had `[tui] steer_key` to pick between the old and the plain bindings. With one set of bindings it has nothing left to pick.

## Tests

- `internal/tui/state/sendkeys_test.go` covers the keys. It checks enter, ctrl+enter, alt+enter, and tab while the agent works and while idle, the empty composer, a queue an interrupt kept, a command sent with tab, shell mode, the agent view, the new-line hint from the terminal's answer, and `/help`.
- `internal/tui/bubble/steer_test.go` runs the real shell over the embedded engine and fakellm. Tab queues two messages, and enter or ctrl+enter on the empty composer sends them in the next request. With and without `tea.KeyboardEnhancementsMsg`, enter reaches the model's next request while a tab-queued message waits for the run's end, and the idle footer names the new-line key.
