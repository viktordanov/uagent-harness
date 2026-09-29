# Integration with the terminal host: design

Status: designed and built, 2026-09-29. The items are in the [ledger](../ledger.md#pending-round-5), round 5.

The terminal host runs coding-agent sessions in tmux-backed terminals that a browser controls. Claude Code, Codex, and OpenCode are interchangeable backends. This record lists what uah adds so that the terminal host can run uah as one more backend, and what uah does not add.

The terminal host facts were checked against the terminal host `1eaf9f3` (2026-09-16).

1. [What the terminal host needs from a harness](#what-the-terminal-host-needs-from-a-harness)
2. [The rule for every change](#the-rule-for-every-change)
3. [Decisions](#decisions)
4. [A session ID chosen at launch](#a-session-id-chosen-at-launch)
5. [Configuration layers](#configuration-layers)
6. [Hooks and subagents](#hooks-and-subagents)
7. [Session lookup](#session-lookup)
8. [A change signal](#a-change-signal)
9. [Deleting a session](#deleting-a-session)
10. [`uah exec`](#uah-exec)
11. [State in the terminal title](#state-in-the-terminal-title)
12. [The session file as a contract](#the-session-file-as-a-contract)
13. [What the terminal host builds](#what-the-terminal-host-builds)
14. [Not done](#not-done)

## What the terminal host needs from a harness

A terminal host provider (`providers.ToolProvider`) does these things for each harness:

- Starts a new session and resumes a session by its native ID.
- Finds the sessions on disk, with their working directory, and reads their messages.
- Gets a cheap signature of a session's storage, to know when it changed.
- Deletes a session.
- Installs hooks that post "turn started", "turn stopped", and "session started" to the terminal host's `/api/notify` endpoint. Busy and idle come from these hooks, not from the screen.

The terminal host runs sessions in three kinds, and each kind gets a different bootstrap:

| Kind | What it is | Bootstrap |
| --- | --- | --- |
| Worker | An ordinary project session | None. Only the machine-wide hooks |
| Anchor | A system session that plans and supervises through the terminal host's full MCP server | A workspace with instructions, skills, notes, and an MCP server |
| Executor ("driver") | A restricted session that drives one delegation graph, with no human present | A workspace with instructions and the driver MCP server, no approvals, trusted |

## The rule for every change

The terminal host depends on Claude Code and Codex, and it keeps one set of mechanisms for every harness: tmux, shell hooks that post to `/api/notify`, `send_to_pty`, and reading the harness's files from disk. A change to uah must make uah more reliable inside those mechanisms. It must not need a new path in the terminal host that exists only for uah.

## Decisions

| Change | Decision |
| --- | --- |
| `--session-id <uuid>` for a new session | Build |
| Configuration layers: `~/.uah/config.d/*.toml` and `UAH_EXTRA_CONFIG` | Build |
| Subagents fire subagent hooks only | Build |
| Session lookup: a complete sidecar, and `uah sessions --workspace --since` | Build |
| A change signal in the sidecar | Build |
| `uah sessions rm <id>` | Build |
| `uah exec`, the headless command, named as Codex names it | Build |
| State and progress in the terminal title | Build |
| The session file documented as a versioned format | Build |
| `--append-instructions <file>` | Not done: the configuration layers and `AGENTS.md` cover it |
| SessionStart that stops the session on `continue: false` | Held |
| A client tag in every hook payload, in place of the terminal host's PID lookup | Not done: see [Hooks and subagents](#hooks-and-subagents) |
| An event stream to a socket while the TUI runs | Not done: a second path beside the hooks |
| A control socket (send input, approve, interrupt) | Not done: the terminal host keeps `send_to_pty` for every harness |
| `uah transcript <id>`, a normalized transcript command | Not done: the terminal host reads files in process |
| A model catalog for the terminal host | Not done: the terminal host solves it on its side |
| A daemon (`uah serve`) that owns sessions | Not considered in this round |

## A session ID chosen at launch

`uah --session-id <uuid>` starts a new session with that ID. An ID that already exists is a usage error, so `--session-id` never resumes; `uah resume <id>` does. The ID must be a UUID. Claude Code has the same flag.

Today a new session always gets a fresh `uuid.NewString()` (`internal/session/session.go`), and the terminal host learns it only from the first Stop or SessionStart hook. Until then the terminal host cannot resume the session, and its discovery can bind the wrong file.

With the flag, the terminal host sets its `ExternalID` before the launch. The hooks still arrive and carry the same ID, so the terminal host's binding code finds a match and does nothing. There is no new path in the terminal host.

`/new` in the TUI still starts a session with a fresh ID, in the same process. Its SessionStart hook reports the new ID with `source = "startup"`. The terminal host handles this as it handles Claude Code's `/clear`.

## Configuration layers

uah reads two more configuration files, both optional:

1. Every `~/.uah/config.d/*.toml`, in lexical order, after the user file.
2. The file that `UAH_EXTRA_CONFIG` names, after `config.d`.

Each layer merges into the result by the rules that apply to a project file today: override, append, OR, or replace by name, as each key's table in [the configuration reference](../configuration.md) says. A layer may set `[projects]`. An unknown key is an error, as in the user file. `uah config` shows which layer set each value.

Precedence, from first to last:

1. A flag.
2. Its environment variable.
3. The resumed session.
4. The project file.
5. `UAH_EXTRA_CONFIG`.
6. `~/.uah/config.d/*.toml`.
7. The user file.
8. The default.

Hooks in `config.d` and `UAH_EXTRA_CONFIG` run as written, like hooks in the user file. A program that can write these files can already write the user file, so they get no trust step.

How the terminal host uses them:

- Machine hooks: the terminal host's `machine_uah.go` writes all of `~/.uah/config.d/host.toml` and replaces it at each start. Today's approach for Codex patches the user's `config.toml` as text under a lock, where an unknown key breaks the file. With a layer, the user's `config.toml` is never touched.
- Anchor: the profile writes one layer file with the terminal host MCP server and the permission settings, and starts uah with `UAH_EXTRA_CONFIG` pointing at it. The workspace has no `.uah/config.toml`, so it needs no `[projects]` trust entry, and nothing goes into the user file.
- Executor: the same, with the driver MCP server (`required = true`), `ask = never`, and the `workspace-write` sandbox. The user's MCP servers and hooks stay loaded, and the terminal host's hooks come from `config.d`.

## Hooks and subagents

A subagent runs in the parent's process with a copy of the parent's hooks (`internal/agents/manager.go`, `childOptions`). Today its SessionStart, UserPromptSubmit, and Stop hooks fire with `session_id = subagent-<uuid>`. The terminal host then binds the terminal host session to the subagent and marks turns started and stopped that belong to the subagent.

The change follows Claude Code:

- SessionStart, SessionEnd, UserPromptSubmit, and Stop fire for root sessions only.
- A new `SubagentStart` event fires when a subagent starts, with `agent_id`, `agent_type`, and `agent_transcript_path`.
- `SubagentStop` stays as it is.
- Tool events (PreToolUse, PostToolUse, PermissionRequest) and PreCompact keep firing inside subagents. When a hook fires inside a subagent, its payload carries `agent_id` and `parent_session_id`, so a hook can tell the two apart.

There is no setting to make subagents fire session hooks.

The terminal host's hook scripts for uah take the same shape as its Codex scripts. The payload fields are the same: `session_id` from the payload, and `pid: $$` from the hook's shell. uah's hook tables are flat (`[[hooks.Stop]]` with `command`), not Codex's nested `hooks = [{type, command}]`.

The terminal host resolves a hook to its session by PID first (`ResolveSessionByPID` in `runtime/pty/registry.go`). It walks up from the hook shell's PID to the PTY root it started. It does this because `HOST_SESSION_ID` is inherited: a `claude` or `uah` started inside another session's shell posts the outer session's ID. uah runs hooks with `/bin/sh` as a child of the uah process, so the walk works without a change. A client tag in the payload would be inherited in the same way, so it does not replace the walk.

## Session lookup

The runner writes `sessions/<id>.session.jsonl`, and its first line holds only the ID and the creation time. The runner stays unchanged, so the lookup fields go into uah's sidecar, `sessions/<id>.uah.json`. The sidecar gets these fields beside `source`, `created`, `parent`, and `settings`:

| Field | Meaning |
| --- | --- |
| `workspace` | The absolute workspace path, as `uah sessions` matches it |
| `first_prompt` | The first user message, cut to 200 characters |
| `last_activity` | The time of the last item |
| `last_sequence` | See [A change signal](#a-change-signal) |

A reader then finds everything about a session from its ID, in one small file, with no walk of `runs/` and no database. Sessions written before the change lack the fields. `uah` fills them in when it resumes such a session, and a reader falls back to `uah sessions --json`.

`uah sessions --json` gets two filters:

- `--workspace <dir>`: sessions of that directory only, matched as `-C` matches today (absolute path, symlinks resolved).
- `--since <RFC 3339 time>`: sessions with activity after that time, for incremental scans.

## A change signal

The sidecar's `last_sequence` is the `Sequence` of the last item in the session file. uah writes it at the end of each turn. It does not change for a turn that is still running, so a reader that needs the live state reads the session file.

The terminal host's storage signature for uah can stay `jsonl:<path>:<size>:<mtime>` of the session file, which it already computes for Codex and Claude Code. `last_sequence` is a cheaper signal that does not change when a file is only touched.

## Deleting a session

`uah sessions rm <id>` deletes a session. It refuses while the session's lock is held, and `--force` overrides that. It removes:

- `sessions/<id>.session.jsonl`, `.uah.json`, `.lock`, `.compaction.jsonl`, and `.agent.json`
- `sessions/operations/<id>/`
- the session's `runs/<run-id>/` directories
- the session's rows in `uah.db`
- its subagents, with the same rules

`--dry-run` prints what it would remove. `--json` prints the removed paths.

## `uah exec`

`uah exec` is the headless command, named as `codex exec`. `uah run` stays as an alias. It adds:

| Flag | Meaning |
| --- | --- |
| `-` as the prompt | Read the whole of stdin as one message. `--stdin` keeps its meaning: one message per line |
| `--ephemeral` | Keep no session: nothing in `sessions/`, `runs/`, or `uah.db` |
| `-o`, `--output-last-message <file>` | Write the final answer to the file |
| `--json` | The same as `--stream` |

The terminal host's quick queries start Codex as `codex exec --json --ephemeral --ignore-user-config --sandbox read-only -c mcp_servers.… -`. The uah equivalent is `uah exec --json --ephemeral --sandbox read-only -`, with the MCP server in a `UAH_EXTRA_CONFIG` file.

## State in the terminal title

Today the TUI sets the title to `uah`. It will show the state:

| State | Title |
| --- | --- |
| Idle | `uah · <workspace name>` |
| Working | `uah · working · <workspace name>` |
| Waiting for an approval | `uah · approve? · <workspace name>` |

It also sends OSC 9;4 progress: indeterminate while working, and cleared when idle. Terminals that do not support the sequence ignore it. `[tui] title = false` turns both off. The terminal host does not depend on this. It helps a person with many tmux panes.

## The session file as a contract

The terminal host reads Claude Code's and Codex's JSONL files in process, and it opens OpenCode's and Forge's SQLite databases read-only in process. No provider starts another program to read history. For uah, the terminal host reads `sessions/<id>.session.jsonl` in the same way.

The file comes from the runner, so uah documents the file and does not change it. The documentation covers:

- The version-2 header line.
- The item kinds a reader needs: `input` (external and control), `turn`, `model_response` with its `message`, `reasoning`, and `tool_call` outputs, and `tool_call_status`.
- `Sequence` as the stable cursor for paging.
- The compaction and rewind records beside the file, which hide items from the model but not from the file.
- Where full tool output lives: `sessions/operations/<id>/`.

A test in uah reads a recorded session file through the documented rules, so a change in the runner's format fails a uah test before it reaches the terminal host. The reader is fast enough for now. The terminal host can add an offset cache later if it needs one.

## What the terminal host builds

This part is the terminal host's work and is listed here to show that the uah changes are enough:

1. A `uah` tool type, its migration, and the lists of tools in REST, MCP, delegation, and the web UI.
2. `providers/uah.go`: the command (`uah --session-id <id> -m … -e …` and `uah resume <id>`), discovery from the sidecars, history from the session file, the storage signature, and deletion through `uah sessions rm`.
3. `bootstrap/machine_uah.go`: writes `~/.uah/config.d/host.toml` with the UserPromptSubmit, Stop, and SessionStart hooks.
4. `bootstrap/profile_uah.go`: the anchor and executor workspaces (`AGENTS.md`, `.agents/skills/`, notes, and the layer file).
5. The quick query, with `uah exec`.

## Not done

- `--append-instructions`: the layers and `AGENTS.md` already give each kind its instructions.
- SessionStart that stops the session: held. The executor's readiness comes from the terminal host's driver MCP `initialize` gate and `required = true`. The TUI connects its MCP servers when the session opens, before any prompt, so the gate opens with no message (ledger item 71); before that, uah connected them on the first message, and a host that waited for `initialize` first never sent one.
- A client tag: the PID walk already finds the right session.
- An event stream and a control socket: both are a second path that only uah would have.
- `uah transcript`: the terminal host reads the file in process.
- A model catalog: the terminal host solves it on its side.
