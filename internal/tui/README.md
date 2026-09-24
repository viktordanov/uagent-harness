<!-- memoria:section id="overview" files="state/state.go state/effects.go render/styles.go bubble/model.go" -->
# TUI

The terminal UI is three packages: a pure reducer (`state`), a pure renderer (`render`), and a thin Bubble Tea v2 shell (`bubble`) that does all I/O.

<!-- memoria:export id="summary" -->
The TUI is a pure reducer from session events and user intents to state and effects, a pure renderer from state to screen lines, and a thin Bubble Tea v2 shell that turns keys into intents and runs the effects against the session. Keys never change meaning: enter queues while the agent works, ctrl+enter sends now, and esc esc interrupts.
<!-- /memoria:export -->

The layout, screens, and framework choice are recorded in the [TUI design](../../docs/design/tui.md); the benchmark behind Bubble Tea v2 is in [bench/tui](../../bench/tui/README.md).

1. [Packages](#packages)
2. [How a message and an effect flow](#how-a-message-and-an-effect-flow)
3. [Transcript items](#transcript-items)
4. [Keys](#keys)
5. [Slash commands](#slash-commands)
6. [Extending the TUI](#extending-the-tui)
7. [Tests](#tests)
<!-- /memoria:section -->

<!-- memoria:section id="packages" files="state/state.go state/reduce.go state/effects.go render/screen.go render/items.go bubble/model.go bubble/effects.go bubble/keys.go" -->
## Packages

| Package | Does | Must not |
| --- | --- | --- |
| `state` | Holds `State` and `Reduce(state, event or intent) (State, []Effect)`. Knows the transcript, the queue, pending approvals, the picker, the menu, and the slash commands | Import Bubble Tea or lipgloss, or do I/O. Effects are values |
| `render` | Draws `State` into lines with lipgloss: `Screen(state, cache, frame)` returns the frame and the composer's row | Import Bubble Tea. It gets the composer's rendered view in `Frame` |
| `bubble` | The Bubble Tea `Model`: maps keys to intents, runs effects as `tea.Cmd`s, batches session events, owns the composer textarea, and draws frames with `render` | Hold UI state of its own beyond the composer, the window size, and the open session |

Because `state` and `render` have no framework code, a different terminal library would replace only `bubble`. `cmd/uah/tui.go` builds `bubble.Deps` (how to open a session, list sessions, and count activity) and calls `bubble.Run`.
<!-- /memoria:section -->

<!-- memoria:section id="flow" files="bubble/model.go bubble/effects.go bubble/keys.go state/reduce.go state/effects.go render/screen.go render/items.go" -->
## How a message and an effect flow

A key press:

1. `bubble.onKey` maps the key to an intent, such as `state.Submit{Text}` for enter. The approval overlay and the picker have their own key maps (`onApprovalKey`, `onPickerKey`), and an open menu takes tab, enter, ↑/↓, and esc first (`menuIntent`).
2. `Model.dispatch` calls `state.Reduce`, which returns the new state and a list of effects, such as `EffSubmit{Text}`.
3. `Model.run` (`bubble/effects.go`) turns each effect into a `tea.Cmd` that calls the session (`Submit`, `SteerNow`, `Interrupt`, `Resolve`, `Compact`, `SetSettings`) or a dependency (`Deps.Sessions`, `Deps.Activity`) off the update loop.
4. An effect that produces data returns it as a message, such as `state.ContextShown`. `Model.Update` routes these message types back to `dispatch`, so the reducer handles them like any other input.

A session event:

1. `onOpened` starts a goroutine that groups `session.Events()` into 16 ms batches, so a burst costs one update and one frame.
2. Each batch arrives as one `eventsMsg`. `Update` reduces every event in order, then waits for the next batch; exactly one wait is pending at a time, which keeps order.
3. Each session gets a generation number. Batches from a closed session are drained and dropped.

A frame: `View` calls `render.Screen`. The transcript is virtualized: it renders items from the bottom up until the window is full. Each item's lines are cached by key, version, width, and view; items that change with time (a running tool, a pending turn) are drawn fresh each frame. A 100 ms tick runs only while something moves on screen.

Effects made before the first session opens (the startup prompt, for example) are held and run once it opens.
<!-- /memoria:section -->

<!-- memoria:section id="items" files="state/items.go state/runevents.go state/agents.go state/contextview.go state/approval.go render/items.go" -->
## Transcript items

The transcript is a list of `Item`s, each with a stable key. The reducer updates an item in place by key and raises its `Version`, so a tool call that finishes after later turns updates its original row.

| Kind | Key | Made from |
| --- | --- | --- |
| `KindUser` | `msg:<input ID>` | `InputSent`, or the runner's `UserMessage` |
| `KindRun` | `run:<run ID>` | `RunStarted`, updated by `RunFinished` |
| `KindTurn` | `turn:<run ID>:<n>` | `TurnStarted`, updated by `ModelResponded` |
| `KindTool` | `call:<call ID>` | `ToolCalled`, `ToolStarted`, `ToolFinished` |
| `KindAssistant`, `KindReasoning` | `text:<n>`, `reason:<n>` | `AssistantMessage`, `ReasoningSummary` |
| `KindNotice` | `notice:<n>` | Session notices, hook results, command output, approvals |
| `KindAgent` | `agent:<ID>` | `engine.AgentUpdated` (a subagent) |
| `KindContext` | `context:<n>` | `/context` (`ContextShown`) |

The compact view draws one line per tool call, as Codex does; the detailed view (ctrl+t) adds the header, run dividers, turns, and token totals. `LevelDebug` notices show only in the detailed view.
<!-- /memoria:section -->

<!-- memoria:section id="keys" files="bubble/keys.go state/reduce.go state/approval.go" -->
## Keys

| Key | Action |
| --- | --- |
| enter | Send. While the agent works, the message queues and goes out when the run ends |
| ctrl+enter, alt+enter | Send now. The embedded engine gives the message to the running agent before its next model request; the process engine restarts the run with the queue and the message |
| shift+enter, ctrl+j | New line |
| esc esc | Interrupt the run (the second esc within 2 seconds); queued messages stay |
| ↑ on an empty composer | Take the last queued message back to edit it |
| alt+, / alt+. | Lower or raise the effort |
| ctrl+s | Session picker |
| ctrl+n | New session |
| `/`, `@` | Open the menu: commands and their values after `/`, workspace files (fuzzy) after `@`. Tab fills in the selection, enter runs a command, esc closes the menu |
| ctrl+t | Compact or detailed view |
| ctrl+r | Show or hide reasoning summaries |
| mouse wheel, shift+↑ / shift+↓, pgup / pgdn | Scroll the transcript. end returns to the bottom. To select text while the TUI reports the mouse, hold Option (iTerm2, Terminal) or Shift (most others) |
| ctrl+c | Clear the composer; on an empty composer, quit (twice while a run is live) |

The approval overlay replaces the composer keys while it is open:

| Key | Answer |
| --- | --- |
| y | Yes, proceed |
| s (or p) | Yes, and don't ask again for commands that start with the proposed prefix. Shown only when there is a prefix |
| n, esc, ctrl+c | No, and tell the agent what to do differently |

In the picker, ↑/↓ choose, enter resumes, tab switches between this directory and all directories, typing filters, and esc goes back.
<!-- /memoria:section -->

<!-- memoria:section id="commands" files="state/commands.go state/menu.go state/context.go state/contextview.go state/mcp.go state/agents.go state/heatmap.go" -->
## Slash commands

`state.Commands()` is the registry; `/help` prints it in this order. A command without "While busy" waits until the agent is idle.

| Command | Does | While busy |
| --- | --- | --- |
| `/model <id>` | Use another model | Yes |
| `/effort <level>` | Set the thinking level: low, medium, high, xhigh, max | Yes |
| `/fast` | Toggle priority processing; needs the embedded engine and the openai or openai-codex provider | Yes |
| `/resume [id]` | Open the picker, or resume a session by ID prefix | No |
| `/new` (`/clear`) | Start a new session | No |
| `/stop` | Interrupt the run; queued messages stay | Yes |
| `/compact` | Compact the context before the next model request (embedded engine) | Yes |
| `/context` | Break down what fills the context window | Yes |
| `/status` | Session, settings, totals, and a 12-week activity heatmap | Yes |
| `/mcp` | MCP servers, their state, and their tools | Yes |
| `/agents` | Subagents and their state | Yes |
| `/sandbox` | The sandbox mode and what commands may do | Yes |
| `/reasoning` | Show or hide reasoning summaries | Yes |
| `/details` | Compact or detailed view | Yes |
| `/help` | Commands and keys | Yes |
| `/quit` (`/exit`) | Close the session and exit | Yes |

`/model`, `/effort`, and `/fast` apply from the next model request on the embedded engine, and from the next run on the process engine; the session's `SettingsChanged` event says which.
<!-- /memoria:section -->

<!-- memoria:section id="extending" files="state/commands.go state/contextview.go state/effects.go state/items.go render/contextview.go render/items.go bubble/effects.go bubble/model.go" -->
## Extending the TUI

To add a slash command that only changes the view, add an entry to `Commands()` in `state/commands.go` with a `run` function that edits the state and returns no effects. `/details` is an example.

To add a command that needs data from the session, follow `/context`:

1. `state/contextview.go`: define the effect (`EffContext`) and the result message (`ContextShown`), the command function that returns the effect, and the reducer handler (`onContextView`). Call the handler from `Reduce` in `state/reduce.go`.
2. `state/commands.go`: add the command to `Commands()`.
3. `bubble/effects.go`: add a case to `Model.run` that calls the session (`sess.ContextUsage()`) and returns the result message.
4. `bubble/model.go`: add the result type to the list in `Update` that routes messages to `dispatch`. A result type missing from that list reaches the composer and is lost.

To add an item kind, follow `KindContext`:

1. `state/items.go`: add the kind and any fields it needs to `Item`.
2. Put items with `State.put` and a key from `nextKey`, or a stable key when later events update the item.
3. `render/items.go`: add a case to `itemLines`, and to `compactLines` when the compact view draws it differently. Put larger drawing code in its own file, as `render/contextview.go` does.
4. If the item changes with time, return true from `Item.Live`, so the cache does not serve a stale frame.

To add a key, map it to an intent in `bubble/keys.go` and handle the intent in `state.Reduce`. Keep the existing keys' meanings.
<!-- /memoria:section -->

<!-- memoria:section id="tests" files="state/reduce_test.go state/menu_test.go state/contextview_test.go render/screen_test.go render/contextview_test.go bubble/bubble_test.go bubble/approval_test.go" -->
## Tests

| Test | Pins |
| --- | --- |
| `state/*_test.go` | The reducer: a run from a captured fixture, tools keeping their place, the queue, keys, commands, history, the picker, the menu, approvals, and `/context` |
| `render/screen_test.go` and the other render tests | Whole screens against golden files in `render/testdata` (`go test ./internal/tui/render -update` rewrites them), and scrolling |
| `bubble/bubble_test.go` | The shell end to end, with real sessions on the process engine and uagent's fake runner: sending, commands, the picker, queue and interrupt, scrolling, and the menu |
| `bubble/approval_test.go` | Approving and declining an escalation, with real sessions on the embedded engine and `testing/fakellm` |
<!-- /memoria:section -->
