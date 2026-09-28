<!-- memoria:section id="overview" files="sessionfile.go data.go" -->
# The session file

unreal-agent-runner writes each session's history to `sessions/<id>.session.jsonl` in uah's home. uah does not change the file. This README documents the file as a versioned format, so a program outside uah can read a session's history without starting uah. This package reads the file by these rules only, with no code from the runner.

<!-- memoria:export id="summary" -->
The runner's session file, `sessions/<id>.session.jsonl`, is a versioned JSON-lines format: a version-2 header, then items numbered by `Sequence`, the stable cursor for paging. uah documents the format and reads it by the documented rules, and a test fails when the runner's output stops following them.
<!-- /memoria:export -->

The rules were checked against unreal-agent-runner v0.1.1, the version in `go.mod`.

1. [The file](#the-file)
2. [Items](#items)
3. [Paging with Sequence](#paging-with-sequence)
4. [Files beside the session file](#files-beside-the-session-file)
5. [Reading it from Go](#reading-it-from-go)
6. [Tests](#tests)
<!-- /memoria:section -->

<!-- memoria:section id="format" files="sessionfile.go data.go" -->
## The file

Each line is one JSON record, `{"type": ..., "data": ...}`. The runner only appends lines. A reader follows these rules:

1. The first line is the header: `{"type":"session","data":{"Version":2,"Session":{"ID":"<id>","CreatedAt":"<time>"}}}`. Stop when `Version` is not 2. Version 1 is an older format that the runner no longer reads.
2. A line of type `item` holds one item in `data.Item`, with `Sequence`, `RecordedAt`, `Kind`, and `Data`.
3. A line of type `operation` updates the state of an operation, such as a running command. A reader of the history skips it.
4. A last line without its newline is a write in progress. Do not read it. Read the file again later.
5. Skip a record type or an item kind that you do not know.

Field names are the runner's Go field names, such as `Sequence` and `RecordedAt`. Times are RFC 3339 in UTC.

## Items

`Sequence` starts at 1 and grows by 1 for each item. It never changes after the runner writes it. `Kind` tells how to read `Data`:

| Kind | `Data` | What a reader shows |
| --- | --- | --- |
| `input` | `ID`, `Kind`, and `Payload` | For `Kind` `external`, a message the user sent: `Payload` is a JSON string with the text. For `control`, a runner instruction, such as a settings change or "stop when idle", with `Payload` `{"Mode", "Reason", "Parameters"}`. A reader of the conversation skips control inputs and `crash` inputs. |
| `turn` | `ID`, `PreviousTurnID`, and `Type` (`regular` or `compaction`) | The start of one model request. The items that follow belong to it. |
| `model_response` | `TurnID` and `Response`: `ID`, `Stop`, `Output`, `Usage`, and `Failure` | What the model returned. `Output` is a list of `{"ProviderID", "Type", "Data"}`. |
| `tool_call_status` | `TurnID`, `CallID`, and `Status` (`Error`, and `WaitingFor`, the operations the call waits for) | The state of a tool call. The line also has `data.Operations`, next to `data.Item`: snapshots of the operations the call starts. |
| `fork` | `ParentID` and `PreviousTurnID` | A subagent forked from its parent. The items before the fork item are copies from the parent's history. |

The output types in `Response.Output`:

| `Type` | `Data` |
| --- | --- |
| `message` | `Role`, `Text`, and `Phase`. `Phase` `final_answer` marks the answer of the turn. |
| `reasoning` | `Summary`, a list of strings, and `Raw`, the provider's own item. |
| `tool_call` | `CallID`, `Name`, and `Arguments`, a JSON text. |

A message with images that the user pasted in the TUI has a tag line at its end for each image, `<uah-image label="[Image #1]" ref="<sha256>.png" …/>`. The image file is in `images/` in uah's home.

## Paging with Sequence

`Sequence` is the cursor. To read in pages, keep the `Sequence` of the last item you read, and next time read only the items with a higher `Sequence`. This is how the runner's own `Items` call pages: the cursor before the first item is 0. Because the runner only appends, a page never changes after you read it.

To find out whether a session changed, compare the last `Sequence`.

## Files beside the session file

These files change what the model sees. They never remove an item from the session file:

- `sessions/<id>.compaction.jsonl`: one line for each compaction, with its `summary`, its `trigger`, and the time `at`. After a compaction, the model sees the summary in place of the older items. A transcript still shows every item, and can show a marker at `at`.
- `sessions/<id>.rewind.jsonl`: one line for each time the user went back to an earlier message, with `message_id`, `from`, and `to`. The items with a `Sequence` from `from` to `to` left the model's context. A transcript can show them as the old branch, or leave them out.
- `sessions/operations/<id>/<operation>/`: the full output of each command, in `out` and `err`. The operation snapshots in the session file give the paths (`State.OutPath` and `State.ErrPath`) and, when the operation is done, its `State.Result`.
<!-- /memoria:section -->

<!-- memoria:section id="usage" files="sessionfile.go data.go" -->
## Reading it from Go

- `Read(path, after, limit)` returns the header and a `Page`: the items with a `Sequence` higher than `after`, at most `limit` of them (all when `limit` is 0), with `Next`, the cursor for the next page, and `More`. It refuses a header with another version (`ErrVersion`).
- `Last(path)` returns the last item. It reads from the end of the file, so its cost does not grow with the history.
- `Item.Decode` and `Output.Decode` decode the data into `Input`, `Turn`, `ModelResponse`, `ToolCallStatus`, `Fork`, `Message`, `Reasoning`, or `ToolCall`. `Operation` reads an operation snapshot.
<!-- /memoria:section -->

<!-- memoria:section id="tests" files="sessionfile_test.go testdata/recorded.session.jsonl" -->
## Tests

`sessionfile_test.go` reads `testdata/recorded.session.jsonl`, a file the embedded engine recorded, by these rules. It finds the user's messages, the Bash call with its result, the reasoning summary, and the answers, pages by `Sequence`, and finds the last item past operation records and a line without its newline. It also reads the same file with the runner's own store and compares the items. `internal/app/sessionfile_test.go` records a new session on the embedded engine at the runner version in `go.mod` and reads it by the same rules, so a change in the runner's format fails a uah test.
<!-- /memoria:section -->
