<!-- memoria:section id="overview" files="state/state.go state/effects.go render/theme.go bubble/model.go" -->
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
6. [The agent view](#the-agent-view)
7. [The look](#the-look)
8. [Extending the TUI](#extending-the-tui)
9. [Tests](#tests)
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

<!-- memoria:section id="items" files="state/items.go state/runevents.go state/agents.go state/contextview.go state/approval.go state/mcp.go render/items.go render/mcp.go" -->
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
| `KindAgent` | `agent:<ID>` | `engine.AgentUpdated` (a subagent): `Name` is the nickname, `Text` the ID, `Detail` the state, and `Agent` the latest update (spawn call ID and message, model, effort, why it failed); its tool calls from `engine.AgentActivity` go into `Sub`, drawn under it in the detailed view. `State.Agents` lists them in start order without walking the transcript |
| `KindContext` | `context:<n>` | `/context` (`ContextShown`) |
| `KindFinish` | `done:<run ID>` | `RunFinished`: the end of a run in the compact view |
| `KindMCP` | `mcp:<n>` | `/mcp` (`MCPListed`): one line per server; `Final` asks for the verbose form (`render/mcp.go`) |

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
| alt+← / alt+→ (alt+b / alt+f on an empty composer) | Switch between the main agent and its subagents, in the order they started |
| ctrl+t | Compact or detailed view |
| ctrl+r | Show or hide reasoning summaries |
| ↑ / ↓ on an empty composer, the mouse wheel, shift+↑ / shift+↓, pgup / pgdn | Scroll the transcript; end returns to the bottom. The TUI leaves the mouse to the terminal, so text selects as usual and the wheel arrives as ↑ and ↓. `[tui] mouse = true` reports the mouse instead: the wheel then scrolls directly, and selecting needs Option (iTerm2, Terminal) or Shift (most others) |
| ctrl+c | Clear the composer; on an empty composer, quit (twice while a run is live) |

The approval overlay replaces the composer keys while it is open:

| Key | Answer |
| --- | --- |
| y | Yes, proceed |
| s (or p) | Yes, and don't ask again for commands that start with the proposed prefix. Shown only when there is a prefix |
| n, esc, ctrl+c | No, and tell the agent what to do differently |

In the picker, ↑/↓ choose, enter resumes, tab switches between this directory and all directories, typing filters, and esc goes back.
<!-- /memoria:section -->

<!-- memoria:section id="commands" files="state/commands.go state/menu.go state/models.go state/context.go state/contextview.go state/mcp.go state/agents.go state/heatmap.go" -->
## Slash commands

`state.Commands()` is the registry; `/help` prints it in this order. A command without "While busy" waits until the agent is idle.

| Command | Does | While busy |
| --- | --- | --- |
| `/model <id>` | Use another model | Yes |
| `/effort <level>` | Set the thinking level: low, medium, high, xhigh, max | Yes |
| `/fast` | Toggle priority processing; needs the embedded engine and the openai or openai-codex provider | Yes |
| `/resume [id]` | Open the picker, or resume a session by ID prefix | No |
| `/new` | Start a new session | No |
| `/clear` | Start the agent fresh in this session: the screen clears, and the next request carries nothing from before; the session keeps its history (embedded engine) | Yes |
| `/stop` | Interrupt the run; queued messages stay | Yes |
| `/compact` | Compact the context before the next model request (embedded engine) | Yes |
| `/context` | Break down what fills the context window | Yes |
| `/status` | Session, settings, totals, and a 12-week activity heatmap | Yes |
| `/mcp [verbose]` | MCP servers: state, transport, tool count, and a login hint; `verbose` (or the detailed view) adds each server's command or URL, auth, and tools with their approval mode | Yes |
| `/agents [name]` | Subagents and their state; with a nickname or ID, that agent's live transcript (see [The agent view](#the-agent-view)) | Yes |
| `/sandbox` | The sandbox mode and what commands may do | Yes |
| `/reasoning` | Show or hide reasoning summaries | Yes |
| `/details` | Compact or detailed view | Yes |
| `/help` | Commands and keys | Yes |
| `/quit` (`/exit`) | Close the session and exit | Yes |

`/model` offers the provider's model list after a space: the first `/model` draft loads it through an effect (`EffLoadModels`, `state/models.go`), off the update loop, from the catalog in `internal/models`. When that list came from the provider, `/model` refuses a model it lacks with the nearest names; with no list, or only the bundled one, any model passes.

`/model`, `/effort`, and `/fast` apply from the next model request on the embedded engine, and from the next run on the process engine; the session's `SettingsChanged` event says which.
<!-- /memoria:section -->

<!-- memoria:section id="agentview" files="state/agentview.go bubble/agentview.go render/agentview.go" -->
## The agent view

`/agents Ada` (or an ID prefix; the menu completes the nicknames) shows a subagent's transcript in place of the session's, under one header line: `agent Ada · alt+← alt+→ switch agents · esc esc interrupts`. It follows Codex's `/subagents` switch, where the TUI shows another thread of the session and the user can type to it. alt+← and alt+→ step through the main agent and the working subagents in the order they started, wrapping around, as Codex's previous- and next-agent keys do; on an empty composer alt+b and alt+f do the same, since many macOS terminals send those for alt+arrows (`SwitchAgent`). A finished or interrupted subagent is not a stop and `/agents <name>` does not open it: its answer is in the main transcript, where its end shows as a line (`Rex completed; the main agent was told`, from the `<subagent_notification>` the main agent got), and `uah sessions show` prints its run.

- `state.AgentView` holds a second `State` for the agent, reduced from its events by the same reducer and drawn by the same renderer, with a cache of its own. The session's own events keep reducing into the main state meanwhile.
- The shell follows the agent with `Session.WatchAgent` (see [internal/session](../session/README.md)): its earlier runs become a `HistoryLoaded`, then its events arrive in 16 ms batches as `state.AgentEvents`, like the session's.
- A message typed in the view goes to the agent (`EffAgentSend`), as the parent's `send_input` does. `/agents` and `/quit` work as usual; ctrl+enter steers the viewed agent's live run (`EffAgentSend.Now`), as it does the main agent's. Other commands are for the main agent and say so. esc esc interrupts the viewed agent while it works (`EffAgentInterrupt`, which stops its own subagents too), as it does the main agent; alt+← back to the main agent, or opening another agent, ends the view. The clock keeps ticking while any subagent runs, so their spinners move while the main agent is idle.
- An approval waiting in the session shows the session's screen until it is answered.
<!-- /memoria:section -->

<!-- memoria:section id="look" files="render/theme.go render/compact.go render/screen.go render/markdown.go bubble/model.go" -->
## The look

The compact view is shaped like Codex's, in amber. The choices came from the style swatchbook and are listed in the [ledger](../../docs/ledger.md) (item 19).

| Part | Drawn as | Code |
| --- | --- | --- |
| Background | The terminal's own; only your messages, the composer, and code blocks sit on a band | `band` in `render/theme.go` |
| Banner | Codex's box at the top of the transcript: `λ uah (version)`, the model with `/model to change`, the directory | `banner` in `render/compact.go` |
| Your messages | `λ ` and the text on the band, with a band row above and below | `userLines` |
| Tool calls | A dim column: label, time, command (`RAN    4.1s   go test ./...`), each field six characters and a space; a live call in the accent (`RUN`), a failed one with `fail` and its detail. Agent tools read `SPAWN`, `SEND`, `WAIT`, `CLOSE`, `RESUME` with the subagents' nicknames instead of their JSON (`SPAWN  Ada · gpt-6-luna low · Summarize…`, `WAIT  Ada, Rex`; `state/agentcalls.go`); other tools the first word of their name (`SkillUse` is `SKILL`), and arguments of one string field show as that string | `compactTool`, `toolLabel`, `toolText` |
| Agent messages | `•` in the accent | `compactLines` |
| Code blocks | On the band, highlighted with the theme's code colors, not wrapped | `markdownLines`, `highlight` |
| Subagents | While running, at the bottom above the working line with a blank line before each: `AGENT Ada  42s`, and under it `└ ⠹ Read …`, its live tool call. A finished one is one line where it was spawned: `done in 1m 12s`, or `failed:` and the provider's reason; closing it afterwards keeps that | `activeAgents`, `agentLines` |
| Working | A breathing `λ` (seven shades, one breath every 1.6 s) and `Working (12s • esc to interrupt)` | `workingLine`, `breathing` |
| A finished run | `12:14 PM · worked 1m 12s`: Codex's time and Claude Code's duration; how it ended first when not ok | `finishLine` (a `KindFinish` item) |
| Composer | `λ ` on the band, with a band row above and below | `Screen`, `composerStyles` in `bubble/model.go` |
| Notices | Plain dim text; warnings start with `!` and errors with `✗` | `itemLines` |
| Footer | Model and effort, directory, context left, hints | `footerLine` |
| `/context` | One dot per percent of the window in its category's color, `·` for free space, `○` for the auto-compaction buffer | `contextLines` |

A `Theme` holds every color: the accent, dim text, the band, the breath's shades, the code colors, and the colors `/context` tells its categories apart with. `Amber` is for dark terminals and `AmberLight` for light ones. The shell asks the terminal for its background at start (`tea.RequestBackgroundColor`); `ThemeFor` picks the theme and tints the band from that background, as Codex tints its message background, and `SetTheme` applies it. A new theme is a new `Theme` value. Text is never colored by the theme, so it keeps the terminal's own foreground.
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
