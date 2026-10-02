# Tool calls in the transcript

Status: built (ledger item 75). The package READMEs hold the current contract; this record keeps the research and the decisions.

How the TUI draws a tool call, and why. Ledger item 75; the [TUI README](../../internal/tui/README.md#the-look) lists the parts and the code.

1. [The problem](#the-problem)
2. [The decision](#the-decision)
3. [The classifier](#the-classifier)
4. [The data](#the-data)
5. [The detailed view](#the-detailed-view)
6. [Limits](#limits)

## The problem

uah 1.4.0 drew every call as one dim line: `RAN` and the command. The model's commands arrive wrapped (`rtk proxy sh -c '…'`) and with absolute paths, so the part that says what happened was cut at the right edge. Every line looked the same, a failure showed only `fail` and `exit N`, an MCP call showed only its JSON arguments, and an auto-approval arrived as a separate notice, often after later calls, repeating the command and wrapping its reason over three lines.

## The decision

The owner picked from a gallery of mock-ups (`cmd-styles.html`, drawn by a generator from uah's layout rules at 110 columns): section "R6, with room", variant (b).

- One line per successful call, in uah's existing column. A command that only reads files is `READ` with the files; one that lists files `LIST`; one that searches `SEARCH` with the pattern and the paths. Any other command stays `RAN`. The text is in the terminal's own color, and separators, line ranges, and globs are dim.
- Commands without their wrappers, with paths relative to the workspace and `~` for the home directory, and a heredoc folded to `python3 <<PY 12 lines · <its lines>`. The freed columns keep commands from being cut.
- Skills loaded one after another share one `SKILL` line.
- An MCP call reads `MCP  server · tool  key "value", key 5`.
- A second line comes only when it says something (R6): why a call failed, in the error color; an MCP call's result, in the theme's comment color; and the auto-approval, `auto-approved · medium risk · <reason>`, on one line in the notices' gray. A successful command gets no preview. The second line starts under the command's text (column 17) with no glyph.
- Spacing (b): a blank line before and after an entry with a second line; runs of one-line entries stay together. The blank lines merge with those around edits and your messages, so there are never two in a row.

The gallery also showed Codex's shape (`• Explored` groups, `• Ran` with output under `└`) and Claude Code's (`● Bash(…)` and `⎿`); the owner kept uah's column and took neither.

## The classifier

`internal/cmdparse` is a port of Codex's `parse_command` at rust-v0.159.1 (`codex-rs/shell-command/src/parse_command.rs`, with `ParsedCommand` from `codex-rs/protocol/src/parse_command.rs`), with its test cases. It splits a script into plain commands joined by `&&`, `||`, `;`, and `|`, drops formatting helpers in a pipeline (`head -n 40`, `wc -l`), follows `cd`, and names each command a read, a listing, a search, or unknown; any unknown command makes the whole script unknown. Codex parses the script with tree-sitter-bash and accepts only literal words; uah parses it with `mvdan.cc/sh`, which the editor already uses, and accepts the same shapes. Words are split and quoted as the `shlex` crate does (ported too). PowerShell, which Codex also reads, is left out: uah runs on macOS and Linux.

uah adds, each marked in the code:

- the files of `cat a b` (Codex calls it unknown), every path operand of a listing or a search (Codex keeps the first, shortened), `rg -g` globs, and the line range of `sed -n a,bp` and `head -n N`, also when piped;
- wrapper stripping before the parse (`Strip`): `rtk proxy`, `rtk <command>` (not rtk's own commands), and a shell running one quoted script (`sh -c`, `bash -lc`, `zsh -lc`), repeatedly;
- relative paths (`Relative`), where a path starts a word or a quoted string, and `git -C .` dropped;
- the heredoc fold, before the parse, since a heredoc makes a script unknown;
- the summary (`Summarize`): a label when every command has the same kind, else `RAN` with the command on one line.

The reducer shapes each call once, when `ToolCalled` arrives (`state/toolcalls.go`), from the call's whole arguments: the runner's label stops at 120 characters.

## The data

The runner's `ToolFinished` carries the exit code or the operation's status, and nothing of the output. The session file has it: a shell operation's state holds its result's stdout and stderr (the head and tail of long output), and an MCP job's state its result text or error. The embedded engine's observer, which already reads each stored item for `PatchApplied`, emits `engine.ToolOutput` once per call: the last 4 KB of a failed command's stderr (its stdout when stderr is empty), or the first 4 KB of an MCP result and its size, or the MCP error. `session.Load` reads the same items, so a resumed transcript shows the same lines. The runner is unchanged. The TUI keeps the last line with a letter or a digit as the error line, and sums up an MCP result as its JSON keys (`{results, total}`), an array's length, or its first line of text, with the size over a kilobyte.

`engine.AutoReviewed` has no call ID: the auto-reviewer sees the approval prompt, which carries only the command. The reducer puts the approval on the latest call among the last 64 items whose whole command is the reviewed one (for MCP, whose tool name starts it). When none matches, the notice stays a line of its own.

A search that exits 1 with no error found nothing, which Codex also does not count as a failure: its line stays dim with `no matches` at the end.

## The detailed view

The detailed view (ctrl+t) keeps the tool's name and the whole command as it ran, not the summary: it is where the user checks what really ran. It shows the same second lines, whole and wrapped, under the command, and follows the same spacing. Skills keep one line each.

## Limits

- The process engine has no observer, so its live runs show no error lines or MCP results; a reloaded transcript does.
- A failed command whose stderr and stdout are empty has no second line.
- An approval for a command that appears twice goes to the latest call without one.
- A mixed script (a read and a search) stays `RAN`, where Codex would list both under `Explored`.
