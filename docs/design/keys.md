# Send keys: queue and send now in every terminal

While the agent works, uah has two ways to send a message: queue it for the end of the run, or send it now, into the live run. Where the terminal tells ctrl+enter from enter, enter queues and ctrl+enter sends now. Where it cannot, enter sends now and tab queues, as in Codex. uah picks the bindings from the terminal's answer to the keyboard enhancement query, and the footer and `/help` show the keys that work.

1. [The problem](#the-problem)
2. [What Codex and Claude Code do](#what-codex-and-claude-code-do)
3. [What the terminal reports](#what-the-terminal-reports)
4. [The bindings](#the-bindings)
5. [Decisions](#decisions)
6. [Tests](#tests)

## The problem

Before this change, enter queued while the agent worked, ctrl+enter or alt+enter sent now, and shift+enter or ctrl+j added a line. tmux has `extended-keys off` by default. With that setting, ctrl+enter reaches the program as plain enter, so "send now" quietly became "queue", and nothing happened until the run ended. The owner runs uah inside a tmux-backed browser terminal, where this happens every time.

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

tmux does not answer the query in any mode. With extended keys on, ctrl+enter arrives, but the message does not, so tmux alone cannot tell uah whether the keys work. Codex's probe gives the same result: to Codex, tmux is a terminal without enhancements.

## The bindings

| Key | The terminal tells ctrl+enter from enter | It cannot (default until it answers) |
| --- | --- | --- |
| enter, agent working | Queue | Send now |
| enter, idle | Send | Send |
| enter, empty composer | Nothing | Send the queued messages now; nothing if none |
| tab, agent working, draft | The composer's | Queue |
| ctrl+enter, alt+enter | Send now; on an empty composer, the queue now | The same |
| shift+enter, ctrl+j | New line | ctrl+j new line; shift+enter arrives as enter where the terminal cannot tell them apart |
| Footer while working | `enter queue · ctrl+enter send now` | `enter send now · tab queue` |
| Queue hint | `ctrl+enter sends now` | `enter sends now` |

`state.Keys` holds the choice. `Keys.Steer` is `[tui] steer_key`, and `Keys.Disambiguated` is the terminal's answer, set by `state.KeyboardReported`, which the shell sends for `tea.KeyboardEnhancementsMsg`. `State.SendIntent(key, draft)` maps enter, tab, ctrl+enter, and alt+enter to `Submit` or `Steer`. In the agent view, "working" means the viewed agent's run.

## Decisions

- **The plain bindings are the default.** Nothing is known until the terminal answers, and it may never answer, so uah starts on enter-sends-now and tab-queues, which work in every terminal. An answer switches to the ctrl+enter bindings, typically within milliseconds of startup, before the first key. No timer is needed.
- **The plain bindings follow Codex.** Enter sends now and tab queues, as Codex does everywhere. uah adds one thing: enter on an empty composer sends the queue now, the plain-key form of ctrl+enter on an empty composer. Idle, a queue an interrupt kept goes the same way.
- **Terminals with enhancements keep the old bindings.** They keep the queue-first behavior that Claude Code also has, and the ctrl+enter the owner already uses.
- **tmux with extended keys on gets the plain bindings.** The message cannot tell uah that tmux passes ctrl+enter through. `$TMUX` would be a hint only, and uah would need to ask tmux for its options. ctrl+enter and alt+enter still send now there, so nothing is lost. `steer_key = "ctrl+enter"` restores the other bindings.
- **Shift+enter is not guarded.** Where the terminal sends shift+enter as enter, the two keys are the same bytes, so enter's meaning applies. The footer and `/help` show ctrl+j as the new-line key in the plain bindings, as Codex's footer does.
- **No warning and no doctor check.** The footer shows the keys that work, and `/help` describes them.
- **An override.** `[tui] steer_key = "auto" | "ctrl+enter" | "enter"`, default `auto`. Any other value stops the TUI with an error.

## Tests

- `internal/tui/state/sendkeys_test.go` covers both bindings. It checks enter, tab, ctrl+enter, and alt+enter while the agent works and while idle, the empty composer, a queue an interrupt kept, shell mode, the agent view, the override, the hints, and `/help`.
- `internal/tui/bubble/steer_test.go` runs the real shell over the embedded engine and fakellm. With `tea.KeyboardEnhancementsMsg`, enter queues and ctrl+enter sends the queue now. Without it, tab queues and enter sends the queue now. Without it, enter also gives a message to the live run while a tab-queued message waits for the run's end.
