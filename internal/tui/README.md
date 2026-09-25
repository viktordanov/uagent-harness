<!-- memoria:section id="overview" files="state/state.go state/effects.go render/theme.go bubble/model.go" -->
# TUI

The terminal UI is three packages: a pure reducer (`state`), a pure renderer (`render`), and a thin Bubble Tea v2 shell (`bubble`) that does all I/O.

<!-- memoria:export id="summary" -->
The TUI is a pure reducer from session events and user intents to state and effects, a pure renderer from state to screen lines, and a thin Bubble Tea v2 shell that turns keys into intents and runs the effects against the session. Keys never change meaning: enter queues while the agent works, ctrl+enter sends now, esc esc interrupts, and ctrl+v pastes an image.
<!-- /memoria:export -->

The layout, screens, and framework choice are recorded in the [TUI design](../../docs/design/tui.md); the benchmark behind Bubble Tea v2 is in [bench/tui](../../bench/tui/README.md).

1. [Packages](#packages)
2. [How a message and an effect flow](#how-a-message-and-an-effect-flow)
3. [Transcript items](#transcript-items)
4. [Keys](#keys)
5. [Slash commands](#slash-commands)
6. [/config](#config)
7. [The agent view](#the-agent-view)
8. [The look](#the-look)
9. [Images](#images)
10. [Plan usage](#plan-usage)
11. [Extending the TUI](#extending-the-tui)
12. [Tests](#tests)
<!-- /memoria:section -->

<!-- memoria:section id="packages" files="state/state.go state/reduce.go state/effects.go render/screen.go render/items.go bubble/model.go bubble/effects.go bubble/keys.go" -->
## Packages

| Package | Does | Must not |
| --- | --- | --- |
| `state` | Holds `State` and `Reduce(state, event or intent) (State, []Effect)`. Knows the transcript, the queue, pending approvals, the picker, the menu, and the slash commands | Import Bubble Tea or lipgloss, or do I/O. Effects are values |
| `render` | Draws `State` into lines with lipgloss: `Screen(state, cache, frame)` returns the frame and the composer's row | Import Bubble Tea. It gets the composer's rendered view in `Frame` |
| `bubble` | The Bubble Tea `Model`: maps keys to intents, runs effects as `tea.Cmd`s, batches session events, owns the composer textarea, and draws frames with `render` | Hold UI state of its own beyond the composer, the window size, and the open session |

When the program ends, `bubble.Run` returns an `Exit` (the open session and its token totals), and `cmd/uah` prints Codex's exit summary from it: the token usage and "To continue this session, run: uah resume <id>", only for a session that ran. Because `state` and `render` have no framework code, a different terminal library would replace only `bubble`. `cmd/uah/tui.go` builds `bubble.Deps` (how to open a session, list sessions, count activity, store pasted images, read the clipboard and copy text to it, and read the plan's usage) and calls `bubble.Run`.
<!-- /memoria:section -->

<!-- memoria:section id="flow" files="bubble/model.go bubble/effects.go bubble/keys.go state/reduce.go state/effects.go render/screen.go render/items.go" -->
## How a message and an effect flow

A key press:

1. `bubble.onKey` maps the key to an intent, such as `state.Submit{Text}` for enter. The approval overlay and the picker have their own key maps (`onApprovalKey`, `onPickerKey`), and an open menu takes tab, enter, ↑/↓, and esc first (`menuIntent`).
2. `Model.dispatch` calls `state.Reduce`, which returns the new state and a list of effects, such as `EffSubmit{Text}`.
3. `Model.run` (`bubble/effects.go`) turns each effect into a `tea.Cmd` that calls the session (`Submit`, `SteerNow`, `SteerQueued`, `Interrupt`, `Resolve`, `Compact`, `Rewind`, `SetSettings`) or a dependency (`Deps.Sessions`, `Deps.Activity`) off the update loop.
4. An effect that produces data returns it as a message, such as `state.ContextShown`. `Model.Update` routes these message types back to `dispatch`, so the reducer handles them like any other input.

A session event:

1. `onOpened` starts a goroutine that groups `session.Events()` into 16 ms batches, so a burst costs one update and one frame.
2. Each batch arrives as one `eventsMsg`. `Update` reduces every event in order and runs the effects they return (a finished run reads the plan's usage), then waits for the next batch; exactly one wait is pending at a time, which keeps order.
3. Each session gets a generation number. Batches from a closed session are drained and dropped.

A streamed answer changes with each batch, so it costs at most one render of its markdown per frame, about 60 a second.

A frame: `View` calls `render.Screen`. The transcript is virtualized: it renders items from the bottom up until the window is full. Each item's lines are cached by key, version, width, and view; items that change with time (a running tool, a pending turn) are drawn fresh each frame. A 100 ms tick runs only while something moves on screen.

Effects made before the first session opens (the startup prompt, for example) are held and run once it opens.
<!-- /memoria:section -->

<!-- memoria:section id="items" files="state/items.go state/runevents.go state/stream.go state/agents.go state/contextview.go state/approval.go state/mcp.go render/items.go render/mcp.go state/patch.go state/shell.go render/shell.go" -->
## Transcript items

The transcript is a list of `Item`s, each with a stable key. The reducer updates an item in place by key and raises its `Version`, so a tool call that finishes after later turns updates its original row.

| Kind | Key | Made from |
| --- | --- | --- |
| `KindUser` | `msg:<input ID>` | `InputSent`, or the runner's `UserMessage`; `Raw` keeps the message as sent, with its image tags. `engine.Rewound` removes it and everything after it |
| `KindRun` | `run:<run ID>` | `RunStarted`, updated by `RunFinished` |
| `KindTurn` | `turn:<run ID>:<n>` | `TurnStarted`, updated by `ModelResponded` |
| `KindTool` | `call:<call ID>` | `ToolCalled`, `ToolStarted`, `ToolFinished`; `engine.PatchApplied` puts an `apply_patch` call's diff in `Diff` (`state/patch.go`) |
| `KindAssistant`, `KindReasoning` | `text:<n>`, `reason:<n>`; `stream:<n>` when streamed | `AssistantMessage`, `ReasoningSummary`; while the model writes them, `engine.TextDelta` and `engine.ReasoningDelta` (below) |
| `KindNotice` | `notice:<n>` | Session notices, hook results, command output, approvals |
| `KindAgent` | `agent:<ID>` | `engine.AgentUpdated` (a subagent): `Name` is the nickname, `Text` the ID, `Detail` the state, and `Agent` the latest update (spawn call ID and message, model, effort, why it failed); its tool calls from `engine.AgentActivity` go into `Sub`, drawn under it in the detailed view. `State.Agents` lists them in start order without walking the transcript |
| `KindContext` | `context:<n>` | `/context` (`ContextShown`) |
| `KindFinish` | `done:<run ID>` | `RunFinished`: the end of a run in the compact view |
| `KindMCP` | `mcp:<n>` | `/mcp` (`MCPListed`): one line per server; `Final` asks for the verbose form (`render/mcp.go`) |
| `KindShell` | `msg:<command ID>` | A command you ran in shell mode: `session.ShellStarted`, `ShellOutput` (streamed into `Detail`), and `ShellFinished`; the runner's echo of its record marks it delivered, and a saved record makes it in a resumed transcript (`state/shell.go`, `render/shell.go`) |

The compact view draws one line per tool call, as Codex does; the detailed view (ctrl+t) adds the header, run dividers, turns, and token totals. `LevelDebug` notices show only in the detailed view.

The answer streams on the embedded engine (`state/stream.go`, the [streaming design](../../docs/design/streaming.md)). Each delta appends to a `KindAssistant` or `KindReasoning` item marked `Streaming`, one per model item (and summary part), which draws like a final message. The runner's `AssistantMessage` then replaces the oldest streamed answer in place, and each `ReasoningSummary` the oldest streamed summary part, so the recorded text wins. `engine.StreamReset` (a retry, a failed or canceled request) and the run's end drop streamed items no final message claimed. `State.Writing` reports an answer in progress, for the working line.
<!-- /memoria:section -->

<!-- memoria:section id="keys" files="bubble/keys.go state/reduce.go state/approval.go state/mode.go state/shell.go render/shell.go bubble/model.go state/backtrack.go render/backtrack.go state/selection.go render/selection.go bubble/mouse.go" -->
## Keys

| Key | Action |
| --- | --- |
| enter | Send. While the agent works, the message queues and goes out when the run ends |
| ctrl+enter, alt+enter | Send now. The embedded engine gives the message to the running agent before its next model request; the process engine restarts the run with the queue and the message. On an empty composer it sends the queued messages now, in order, the same way, also a queue an interrupt kept (`EffSteerQueued`, `Session.SteerQueued`); with nothing queued it does nothing |
| shift+enter, ctrl+j | New line |
| esc esc | Interrupt the run (the second esc within 2 seconds); queued messages stay. It also stops a running shell-mode command. While idle with nothing queued, on an empty composer, it goes back to an earlier message instead (below) |
| `!` on an empty composer | Shell mode (see below) |
| ↑ on an empty composer | Take the last queued message back to edit it |
| alt+, / alt+. | Lower or raise the effort |
| shift+tab | Next permission mode: read only, workspace, auto, and back to read only; from full access, read only. The footer shows the mode, and the session applies it (live on the embedded engine, from the next run on the process engine); `state/mode.go` |
| ctrl+s | Session picker |
| ctrl+n | New session |
| `/`, `@` | Open the menu: commands and their values after `/`, workspace files (fuzzy) after `@`. Tab fills in the selection, enter runs a command, esc closes the menu |
| alt+← / alt+→ (alt+b / alt+f on an empty composer) | Switch between the main agent and its subagents, in the order they started |
| ctrl+t | Compact or detailed view |
| ctrl+r | Show or hide reasoning summaries |
| ↑ on the composer's first row and ↓ on its last (always, for a one-line prompt), the mouse wheel, shift+↑ / shift+↓, pgup / pgdn | Scroll the transcript; end returns to the bottom |
| drag, double click, triple click | Select transcript text: from cell to cell, a word, or a line; letting go copies it (see below). Esc or a click clears the selection. The terminal's own selection needs Option (iTerm2, Terminal) or Shift (most others); `[tui] mouse = false` leaves the mouse to the terminal, so text selects as usual and the wheel arrives as ↑ and ↓ |
| ctrl+c | Clear the composer; on an empty composer, quit (twice while a run is live) |
| ctrl+v, alt+v | Paste the clipboard's image as `[Image #N]` at the cursor, on macOS and Linux, as Codex and Claude Code do. A clipboard without an image pastes its text. See [Images](#images) |
| backspace after `[Image #N]` | Delete the whole placeholder and its image |

The approval overlay replaces the composer keys while it is open:

| Key | Answer |
| --- | --- |
| y | Yes, proceed |
| s (or p) | Yes, and don't ask again for commands that start with the proposed prefix. Shown only when there is a prefix |
| n, esc, ctrl+c | No, and tell the agent what to do differently |

**Shell mode**, after Codex's and Claude Code's `!`: the composer's λ becomes an accent `!`, the placeholder and the footer's hint say so, and the menu stays closed, so `/bin/ls` is a path. Enter (or ctrl+enter) runs the line as a command in the workspace (`EffShell`, `Session.RunShell`) and returns the composer to messages; backspace on the empty composer, or esc, leaves shell mode first. The command runs at once, also while the agent works; its output streams into a `KindShell` item, drawn on the band after `!` with its exit status and its output folded to 10 rows (50 in the detailed view). Its record goes to the agent with your next message. `State.Shell` holds the mode, `render.ShellPrompt` draws the mark, and `bubble` only maps `!` and backspace and applies the prompt after each reduce. The research and decisions are in the [shell mode design](../../docs/design/shell-mode.md).

**Going back to an earlier message**, as Codex's backtrack (`state/backtrack.go`): while the session is idle with nothing queued, esc on an empty composer primes it ("esc again to edit a previous message"), and a second esc within 2 seconds, or `/rewind`, selects your latest delivered message. The renderer marks it `▶ … ↵ edit from here`, scrolls it a third of the way down the window, and draws the rest of the transcript in the theme's dim color, also what the cut would drop (`render/backtrack.go`). Fading is a pass over the window's lines each frame: it keeps backgrounds, such as the band, and turns every other color and weight to the dim, so the cache keeps each item's own lines and entering or leaving the selection draws no item again. Esc, ↑, and ← select an earlier message; ↓ and → a later one; ctrl+c cancels, and any other key cancels and then does what it does. Enter returns `EffRewind` and puts the message, with its images (`Item.Raw` keeps its image tags), in the composer; the shell calls `Session.Rewind`. When `engine.Rewound` arrives, the reducer drops the message and every item after it, and the footer's meter takes the cut's tokens; a resumed transcript applies the same event after the run it followed. Since it needs an idle session, no streaming item exists when the cut happens. The process engine lacks it: `/rewind` shows the capability table's notice. The [rewind design](../../docs/design/rewind.md) has the research and the decisions.

**Selecting text**, as a terminal selects it, while the TUI reports the mouse (`[tui] mouse`, on by default; the [selection design](../../docs/design/selection.md) has the research and the decisions). A press and a drag on the transcript select from cell to cell, a double click selects a word (a run of non-space), and a triple click a line; letting go copies the text and the footer says `copied 3 lines`. Positions are an item's key, a line of its drawing, and a cell (`state.TextPos`), not screen rows, so a selection stays on its text while the transcript scrolls or streams. Dragging to the transcript's top row, or below it, scrolls a line per move, and the wheel keeps scrolling during a drag, with the selection following the text under the mouse. Esc clears the selection and does nothing else; a click elsewhere, typing, sending, ctrl+t, and ctrl+r clear it too. The composer and the panels take no selection, nor do the picker and the agent view.

- **State** (`state/selection.go`): `MousePress` (with the line's text and the time, which count the clicks), `MouseDrag`, `MouseRelease`, and `ClearSelection` make `State.Selection`; the release returns `EffCopySelection`, and `Copied` shows the notice for two seconds.
- **Render** (`render/selection.go`): each frame records the transcript line every window row shows, so `Cache.At` turns a screen cell into a position and `Cache.Edge` tells a drag past the top or bottom. The selection is drawn as a pass over the window's lines, as going back fades them: the selected cells lose their styles and take the theme's `Selection` background, so the cache keeps each item's own lines. `SelectedText` copies what is drawn, cut by cells (a wide character half inside counts whole), minus decoration: the λ or ! column and the indent under it, the answer's • column, reasoning's `~`, a warning's mark, and on a code line the padding before its text and the fence's language; trailing spaces and blank lines at either end go.
- **Shell** (`bubble/mouse.go`): maps the mouse to the intents and runs the copy: OSC 52 (`tea.SetClipboard`), which the terminal handles, also over ssh, and `Deps.CopyText`, the system's tool (`internal/images/clipboard.TextWriter`: pbcopy, wl-copy, or xclip; none is fine), off the update loop.

In the picker, ↑/↓ choose, enter resumes, tab switches between this directory and all directories, typing filters, and esc goes back.

In the `/config` panel, ↑/↓ choose a setting, enter or space changes it, ←/→ cycle back and forth, and esc closes; while a value is being typed, enter saves it and esc cancels (see [/config](#config)).
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
| `/rewind` | Select your latest message to go back to, as esc esc does while idle (see [Keys](#keys); embedded engine) | No |
| `/compact [focus]` | Compact the context before the next model request; words after it tell the summary what to focus on, as Claude Code's `/compact [instructions]` (embedded engine) | Yes |
| `/context` | Break down what fills the context window | Yes |
| `/config` | The settings panel: change the basic settings and save them to the user file (see [/config](#config)) | Yes |
| `/status` | Session, settings, totals, what the engine runs without (from its capabilities), a 12-week activity heatmap, and the plan's usage (see [Plan usage](#plan-usage)) | Yes |
| `/usage` | Your plan's usage, read fresh: each limit with a bar, what is left, and when it resets, as `uah usage` prints it (openai-codex) | Yes |
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

<!-- memoria:section id="config" files="state/config.go state/configrows.go render/config.go bubble/keys.go bubble/effects.go" -->
## /config

`/config` opens a panel above the composer, after Claude Code's `/config`: the basic settings, one row each, with the value a session opened now would use and where it comes from, as `uah config` explains them (`app.Explain`). The selected row is in the accent band; the title names the user file changes go to.

| Row | Key | Change | Applies |
| --- | --- | --- | --- |
| Auto-compact | `auto_compact_percent` | Cycles off, 50, 60, 70, 80, 85, 90, 95% | New sessions |
| Auto-compact token limit | `model_auto_compact_token_limit` | Type a number; empty or 0 removes it | New sessions |
| Compaction model | `compact_model` | Cycles the session's model and the provider's models; the session's model removes the key | New sessions |
| Model | `model` | Cycles the provider's models, or type one when there is no list | This session too, as `/model` |
| Effort | `effort` | Cycles low to max | This session too, as `/effort` |
| Fast mode | `fast` | Toggles | This session too, as `/fast`, where the engine has it |
| Permission mode | `permission_mode` | Cycles read only, workspace, auto, as shift+tab; full access stays a value for the file | This session too, as shift+tab |
| Details view | `[tui] details` | Toggles | At once |
| Mouse | `[tui] mouse` | Toggles | At once |

The split follows the rest of the TUI:

- **State** (`state/config.go`, `state/configrows.go`): `ConfigPanel` holds the loaded values, the selected row, and a value being typed. The reducer turns a change into `EffSaveConfig`, shows the new value at once with the user file as its source, and, for a setting the session takes live, adds the same `EffSetSettings` that `/model`, `/effort`, `/fast`, and shift+tab send. Opening the panel loads the values (`EffLoadConfig`) and the model list (`EffLoadModels`).
- **Render** (`render/config.go`): the rows, the value in bold, on in the good color, the source dim, and the keys at the bottom.
- **Effects** (`bubble/effects.go`): `Deps.Config` and `Deps.SaveConfig`, which `cmd/uah` backs with `app.Inspect` and `app.SaveSetting`. A save goes through the comment-preserving editor that `uah mcp add` uses (`internal/config/tomledit`); a change that would stop a session from starting, such as fast mode with the process engine, is undone and reported. After each save the panel reloads, and says so when a flag, the environment, or a trusted project file still sets the key and wins over the user file.
<!-- /memoria:section -->

<!-- memoria:section id="agentview" files="state/agentview.go bubble/agentview.go render/agentview.go" -->
## The agent view

`/agents Ada` (or an ID prefix; the menu completes the nicknames) shows a subagent's transcript in place of the session's, under one header line: `agent Ada · alt+← alt+→ switch agents · esc esc interrupts`. It follows Codex's `/subagents` switch, where the TUI shows another thread of the session and the user can type to it. alt+← and alt+→ step through the main agent and the working subagents in the order they started, wrapping around, as Codex's previous- and next-agent keys do; on an empty composer alt+b and alt+f do the same, since many macOS terminals send those for alt+arrows (`SwitchAgent`). A finished or interrupted subagent is not a stop and `/agents <name>` does not open it: its answer is in the main transcript, where its end shows as a line (`Rex completed; the main agent was told`, from the `<subagent_notification>` the main agent got), and `uah sessions show` prints its run.

- `state.AgentView` holds a second `State` for the agent, reduced from its events by the same reducer and drawn by the same renderer, with a cache of its own. The session's own events keep reducing into the main state meanwhile.
- The shell follows the agent with `Session.WatchAgent` (see [internal/session](../session/README.md)): its earlier runs become a `HistoryLoaded`, then its events arrive in 16 ms batches as `state.AgentEvents`, like the session's.
- A message typed in the view goes to the agent (`EffAgentSend`), as the parent's `send_input` does. `/agents` and `/quit` work as usual; ctrl+enter steers the viewed agent's live run (`EffAgentSend.Now`), as it does the main agent's, and on an empty composer sends the agent's own queued messages now (`EffAgentSteerQueued`). Other commands are for the main agent and say so. esc esc interrupts the viewed agent while it works (`EffAgentInterrupt`, which stops its own subagents too), as it does the main agent; alt+← back to the main agent, or opening another agent, ends the view. The clock keeps ticking while any subagent runs, so their spinners move while the main agent is idle.
- An approval waiting in the session shows the session's screen until it is answered.
<!-- /memoria:section -->

<!-- memoria:section id="look" files="render/theme.go render/compact.go render/screen.go render/markdown.go render/markdown/markdown.go render/markdown/blocks.go render/markdown/inline.go render/markdown/table.go render/markdown/highlight.go render/markdown/code.go render/markdown/quote.go bubble/model.go render/diff.go render/words.go state/context.go render/backtrack.go render/selection.go" -->
## The look

The compact view is shaped like Codex's, in amber. The choices came from the style swatchbook and are listed in the [ledger](../../docs/ledger.md) (item 19).

| Part | Drawn as | Code |
| --- | --- | --- |
| Background | The terminal's own; only your messages, the composer, code blocks, and every other table row sit on a band | `band` in `render/theme.go` |
| Banner | Codex's box at the top of the transcript: `λ uah (version)`, the model with `/model to change`, the directory | `banner` in `render/compact.go` |
| Your messages | `λ ` and the text on the band, with a band row above and below | `userLines` |
| Tool calls | A dim column: label, time, command (`RAN    4.1s   go test ./...`), each field six characters and a space; a live call in the accent (`RUN`), a failed one with `fail` and its detail. Agent tools read `SPAWN`, `SEND`, `WAIT`, `CLOSE`, `RESUME` with the subagents' nicknames instead of their JSON (`SPAWN  Ada · gpt-6-luna low · Summarize…`, `WAIT  Ada, Rex`; `state/agentcalls.go`); other tools the first word of their name (`SkillUse` is `SKILL`), and arguments of one string field show as that string | `compactTool`, `toolLabel`, `toolText` |
| File edits | An `apply_patch` call as `EDIT` with Codex's `Edited path (+3 -1)` (`Added`, `Deleted`, or `Edited 2 files` with a `└ path` line per file), then the changed lines: the line number in a dim gutter, added lines tinted green and removed ones red across the whole line, the changed words inside a replaced line marked in a stronger tint, context lines dim, and `⋮` between hunks. The compact view folds after 12 lines with `… +N lines (ctrl+t to view)`; the detailed view shows the whole diff | `patchLines`, `diffBlock`, `diffLine` in `render/diff.go`; `wordDiff` in `render/words.go` |
| Agent messages | `•` in the accent, then the Markdown: `code` in the code color, bold, italic, strikethrough, H1 in the accent and in capitals, H2 in the accent, other headings in bold, dim `•` bullets (`◦` when nested) and dim numbers right-aligned so the items' text lines up, with a hanging indent, task lists as `[x]`, quotes indented in dim italic between dim `“ ”` marks, GitHub alerts (`> [!WARNING]`) as a bold `! Warning` title in the alert's color over their indented text, dim rules, links as the text with the URL dim after it, and one blank line between blocks | `compactLines`, `markdownLines`; package `render/markdown` |
| Code blocks | On the band, highlighted with the theme's code colors, not wrapped, with the fence's language dim at the right end of the first line; a fence without a known language in the code color; a `diff` block's `+` and `-` lines on the edit tool's tints | `codeBlock`, `highlight` in `render/markdown` |
| Tables | Zebra rows: columns two apart with no rules, the header in the accent, every other row on the band, cells wrapped to fit the width; too narrow for that, each row as records (`Header  value`), every other one on the band | `table.go` in `render/markdown` |
| Subagents | While running, at the bottom above the working line with a blank line before each: `AGENT Ada  42s`, and under it `└ ⠹ Read …`, its live tool call. A finished one is one line where it was spawned: `done in 1m 12s`, or `failed:` and the provider's reason; closing it afterwards keeps that | `activeAgents`, `agentLines` |
| Working | A breathing `λ` (seven shades, one breath every 1.6 s) and `Working (12s • esc to interrupt)`; `Thinking` while a model request is out, `Writing` while its answer streams, `Running 2 commands` while tools run | `workingLine`, `breathing`, `statusLine` |
| Reconnecting | While a model request waits to be sent again (`engine.Reconnecting`, embedded engine), the working line reads `Reconnecting, attempt 3 of 10 (retrying in 8s • esc to interrupt)`, counting down, then `(connecting • …)` while the attempt is in flight; the detailed view shows it in the footer. `State.Live.Reconnect` holds it until `engine.ReconnectEnded`, a response, or the run's end, and each retry leaves a `LevelDebug` notice with its reason | `statusLine`, `reconnectText`; `state/context.go` |
| A finished run | `12:14 PM · worked 1m 12s`: Codex's time and Claude Code's duration; how it ended first when not ok | `finishLine` (a `KindFinish` item) |
| Composer | `λ ` before its first row only (the rows under it line up with the text), on the band, with a band row above and below | `Screen`, `composerStyles` in `bubble/model.go` |
| Going back | The selected message as `▶ … ↵ edit from here` on the band, the label on the accent; the rest of the transcript faded to the dim color, keeping only backgrounds | `transcriptLines`, `fade` in `render/backtrack.go` |
| Selected text | The cells on the theme's selection background, in the terminal's own text color | `highlight` in `render/selection.go` |
| Notices | Plain dim text; warnings start with `!` and errors with `✗` | `itemLines` |
| Footer | Model and effort, fast, the permission mode (`read only mode`, `workspace mode`, `auto mode`, or `full access mode`), directory, the plan's tightest window (`weekly 78% left`, hidden without usage), context left, hints. The detailed view's header shows the mode too | `footerLine`, `modeText` |
| `/context` | One dot per percent of the window in its category's color, `·` for free space, `○` for the auto-compaction buffer | `contextLines` |
| `/config` | A title in the accent, a row per setting with the selected one in the accent band, and the keys dim at the bottom | `configLines` |

Markdown goes through `render/markdown`: goldmark parses it with the GFM extensions, and its own renderer walks the tree. Each `Styles` owns one `markdown.Renderer`, which keeps the finished top-level blocks of the messages it drew and the highlighted code, so a message that grows (a streaming answer) re-renders only its last block, and a width change renders once more at the new width. The [Markdown design](../../docs/design/markdown.md) has the reasons and the numbers.

A `Theme` holds every color: the accent, dim text, the band, the breath's shades, the code colors, the diff tints (Codex's dark tints in `Amber`, GitHub's light ones in `AmberLight`), which diff code blocks share, the selection's background, and the colors `/context` tells its categories apart with, which also color GitHub alerts' titles. `Amber` is for dark terminals and `AmberLight` for light ones. The shell asks the terminal for its background at start (`tea.RequestBackgroundColor`); `ThemeFor` picks the theme and tints the band from that background, as Codex tints its message background, and the shell makes a new render cache for it (`NewCache(theme)`). The cache holds the theme's `Styles`, and every drawing function is a method of `Styles`, so there is no shared styling state: two caches draw two themes side by side, and render tests can run in parallel. A new theme is a new `Theme` value. Text is never colored by the theme, so it keeps the terminal's own foreground, except while going back, when everything but the selected message is dim.
<!-- /memoria:section -->

<!-- memoria:section id="images" files="state/images.go bubble/images.go bubble/keys.go bubble/model.go state/menu.go" -->
## Images

The composer takes images as Codex's does; the [images design](../../docs/design/images.md) has the research and the reasons.

1. ctrl+v or alt+v (`PasteImage`) runs `EffPasteImage`: the shell reads the clipboard through `Deps.Clipboard` (`internal/images/clipboard`: osascript on macOS, wl-paste or xclip on Linux) and stores the image through `Deps.Images` (`internal/images.Store`), off the update loop. A clipboard without an image falls back to the textarea's text paste.
2. A bracketed paste that is one image file's path (quoted, shell-escaped, `file://`, or `~/`), which is also what a terminal pastes for a dropped file, becomes `AttachFile`; choosing an image file after `@` does the same in place of its path (`acceptImage`).
3. `ImageAttached` adds the image to `State.Attached` with the next label, and `EffInsertText` puts `[Image #N] ` at the cursor. `ImageFailed` shows a warning and pastes the text it came from.
4. `DraftChanged` drops the images whose placeholder is gone. Backspace at the end of a placeholder first deletes the rest of it (`eatPlaceholder`), so one key removes it. Numbers are not reused within a draft and not renumbered.
5. Submit and steer join the draft's images to the message as tag lines (`images.Join`). The transcript, the queue, and a resumed session show the text without them (`images.Display`), and ↑ takes a queued message back with its images (`DraftRestored`).

When the engine lacks `Images` (the process engine), a pasted image or path shows the capability table's notice, and a path stays text.
<!-- /memoria:section -->

<!-- memoria:section id="usage" files="state/usage.go bubble/usage.go render/screen.go state/commands.go" -->
## Plan usage

The ChatGPT plan's usage (see [internal/usage](../usage/README.md)) shows in four places. `cmd/uah` passes the session's reader in `Deps.Usage`; without one, the TUI has no usage, as for a provider without it.

| Place | Shows | Read |
| --- | --- | --- |
| `/status` | A notice with the plan and a row per window: `weekly [███████████████░░░░░] 78% left (resets 15:44 on 26 Sep)`. When the read fails, the error and the last snapshot, marked `stale` after 15 minutes. For another provider, "usage is not available for <provider>" | On each `/status` (max age 0) |
| Footer | The tightest window before the context meter: `weekly 78% left · 64% context left`. Hidden before the first read and for a provider without usage | After each run (`RunFinished`), cached 60 s |
| Warnings | "Heads up, you have less than 25% of your weekly limit left (resets …)", once per window when it passes 75, 90, and 95% used; a window that resets warns again | The read after each run |
| Limit reached | "Usage limit reached; try again at 15:44 on 26 Sep.", once per run, when a `RunnerError` or a model failure is the usage limit (`usage.LimitReachedIn`). The time comes from the failure text when it kept it, else from a fresh read; without one, the notice links ChatGPT's usage page | On that failure (max age 0) |

The state holds `State.Usage`: the last snapshot, whether the provider has none, and the thresholds already warned. The reducer asks for a read with `EffLoadUsage{Reason, MaxAge}`, from `usageAfter` (which `Reduce` calls for each session event) and from `/status`. `bubble/usage.go` calls the reader off the update loop, and the answer comes back as `UsageLoaded`, which `onUsage` handles by its reason. There is no timer.
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

<!-- memoria:section id="tests" files="state/images_test.go bubble/images_test.go state/reduce_test.go state/menu_test.go state/contextview_test.go state/mode_test.go render/screen_test.go render/contextview_test.go render/mode_test.go bubble/bubble_test.go bubble/approval_test.go bubble/mode_test.go state/config_test.go bubble/config_test.go render/diff_test.go state/shell_test.go render/shell_test.go bubble/shell_test.go state/usage_test.go render/usage_test.go bubble/usage_test.go state/agents_test.go bubble/steer_test.go state/reconnect_test.go render/reconnect_test.go state/stream_test.go render/stream_test.go bubble/stream_test.go render/markdown/markdown_test.go render/markdown/incremental_test.go render/markdown_test.go render/markdown_bench_test.go state/backtrack_test.go render/backtrack_test.go render/backtrack_internal_test.go bubble/rewind_test.go state/selection_test.go render/selection_test.go bubble/selection_test.go" -->
## Tests

| Test | Pins |
| --- | --- |
| `state/*_test.go` | The reducer: a run from a captured fixture, tools keeping their place, the queue, keys, commands, history, the picker, the menu, approvals, and `/context` |
| `render/screen_test.go` and the other render tests | Whole screens against golden files in `render/testdata` (with a diff in both views, `patch` and `patch-details`, and its tints in `render/diff_test.go`) (`go test ./internal/tui/render -update` rewrites them), and scrolling |
| `render/markdown/markdown_test.go`, `render/markdown/incremental_test.go`, `render/markdown_test.go`, `render/markdown_bench_test.go` | Markdown: every construct against goldens at several widths (`render/markdown/testdata`, `-update` rewrites them), alerts, diff blocks, and right-aligned numbers among them, zebra tables down to records, highlighting and its cache, incremental rendering equal to full rendering for every prefix, each update parsing only the last block, each theme's colors, and the benchmarks (`go test -run '^$' -bench Markdown -benchmem ./internal/tui/render`) |
| `bubble/bubble_test.go` | The shell end to end, with real sessions on the process engine and uagent's fake runner: sending, commands, the picker, queue and interrupt, scrolling, and the menu |
| `state/reduce_test.go`, `state/agents_test.go`, `bubble/steer_test.go` | ctrl+enter on an empty composer: the queue goes now in order under its IDs, also a queue an interrupt kept, nothing without a queue or in shell mode, the viewed agent's own queue, and a real embedded session whose working agent reads both queued messages before its next model request |
| `bubble/approval_test.go` | Approving and declining an escalation, with real sessions on the embedded engine and `testing/fakellm` |
| `state/mode_test.go`, `render/mode_test.go`, `bubble/mode_test.go` | shift+tab's cycle, the mode notice, the footer and header in each mode, and shift+tab through a real session |
| `state/images_test.go`, `bubble/images_test.go` | Images: placeholders and their numbers, removal by deleting the placeholder or with one backspace, what a message sends, the transcript with placeholders live and resumed, pasted and dropped paths, `@` image files, the process engine's notice, and a real embedded session whose model request carries the image, with a fake clipboard |
| `state/usage_test.go`, `render/usage_test.go`, `bubble/usage_test.go` | Plan usage: the read after each run, `/status` rows and staleness, the footer, warnings once per threshold, the limit notice, a provider without usage, and a real reader against a loopback backend |
| `state/shell_test.go`, `render/shell_test.go`, `bubble/shell_test.go` | Shell mode: entering and leaving it, enter running the line, `/` as a path, the `!` prompt and the footer, the item from its events, the echo, and a resumed record (golden `shell`), and `!` through a real session on the process engine |
| `state/reconnect_test.go`, `render/reconnect_test.go` | A retry held in `Live.Reconnect` until it ends, a response arrives, or the run finishes, its detail notice, and the working line's countdown, `connecting`, and the detailed footer |
| `state/stream_test.go`, `render/stream_test.go`, `bubble/stream_test.go` | Streaming: deltas growing an item in place, the final message replacing it, a reset and the run's end dropping it, a streamed answer drawn as a final one with `Writing` in the working line, and a real embedded session whose answer shows while the response is held open |
| `state/backtrack_test.go`, `render/backtrack_test.go`, `bubble/rewind_test.go` | Going back: priming, selecting, stepping, cancelling, the composer with the message and its image, the cut on `engine.Rewound` and in a resumed transcript, no backtrack while busy (also while an answer streams), with a draft, without messages, or on the process engine, the marked message and the scroll (goldens `backtrack-latest`, `backtrack-older`), the rest faded in both themes and back after cancelling (`backtrack-faded`, `backtrack-faded-light`, a mark per line), the cache untouched (`render/backtrack_internal_test.go`, with `BenchmarkBacktrackFrame`), and a real embedded session whose next request carries only the history before the edited message |
| `state/selection_test.go`, `render/selection_test.go`, `bubble/selection_test.go` | Selecting text: press, drag, and release in either direction, a click selecting nothing, double and triple clicks (cells of wide characters, a slow click), clearing on esc, a click, typing, sending, and view changes, the selection staying on its text while the transcript scrolls and streams and going with its items, the copy notice, the overlay in both themes and the lines as before once cleared (`selection`, `selection-light`), what a copy holds (the band, a code line, wide characters, trailing spaces), the screen-to-text mapping, and a real session: a drag, a double click, esc, a click on the composer, a drag past the top scrolling, and the wheel during a drag, with the copies caught by `Deps.CopyText` |
| `state/config_test.go`, `bubble/config_test.go` | `/config`: the rows and sources, toggles, cycles, typed values, what applies live, the warning when another source wins, and a real user file saved with its comments kept, with a change that would stop a session from starting undone |
<!-- /memoria:section -->
