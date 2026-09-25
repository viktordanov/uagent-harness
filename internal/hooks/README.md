<!-- memoria:section id="overview" files="hooks.go exec.go" -->
# Hooks

Hooks run a user's command at a session event, with Claude Code's contract, so existing hook scripts work unchanged. This package runs the commands, combines their decisions, and keeps the trust store for project hooks. The session and the embedded engine decide when each event fires.

<!-- memoria:export id="summary" -->
Hooks run a command at a session event with Claude Code's contract: the event arrives as JSON on stdin, exit 0 continues, exit 2 blocks with stderr as the reason, and any other exit is reported and ignored. Project hooks run only after `uah hooks trust` records their exact commands and the content of any local script they run.
<!-- /memoria:export -->

Hooks are `[[hooks.<Event>]]` entries in the configuration; the keys are in the [configuration reference](../../docs/configuration.md#hooks). `uah hooks` lists the hooks for a workspace and whether each runs.

1. [Events](#events)
2. [Running a hook](#running-a-hook)
3. [Output and decisions](#output-and-decisions)
4. [Trust](#trust)
5. [Tests](#tests)
<!-- /memoria:section -->

<!-- memoria:section id="events" files="hooks.go payload.go" -->
## Events

```toml
[[hooks.PreToolUse]]            # embedded engine only
matcher = "Bash"                # a regular expression on the whole tool name
command = "~/.uah/hooks/no-rm-rf.sh"
timeout = "10s"                 # default 60s

[[hooks.Stop]]
command = "osascript -e 'display notification \"uah is idle\"'"
```

| Event | Runs | A hook can | Runs in |
| --- | --- | --- | --- |
| `SessionStart` | When the session opens (`source`: startup or resume) | Add context to the first message (`additionalContext` or plain stdout); show a `systemMessage` | session |
| `UserPromptSubmit` | Before a message is sent | Block it (exit 2 or `"decision": "block"`); add context | session |
| `PreToolUse` | Before each tool call | Deny it (exit 2, or `permissionDecision` `"deny"` or `"ask"`); the reason is the tool's error. Rewrite it (`updatedInput`) | embedded engine |
| `PostToolUse` | After each tool call | Observe only | session |
| `Stop` | When the agent finished and nothing is queued | Keep it going: `"decision": "block"` with a `reason` sends the reason as the next message, at most 5 times in a row. `stop_hook_active` is true after the first | session |
| `SubagentStop` | When a subagent finished (`agent_id`, `agent_type`, `agent_transcript_path`, `last_assistant_message`; `session_id` is the parent's) | Keep it going: `"decision": "block"` with a `reason` sends the reason to the subagent, at most 5 times in a row | internal/agents |
| `PermissionRequest` | Before the user is asked to approve an escalated command, a `prompt` rule, or an MCP call | Answer for the user with `permissionDecision` `"allow"` or `"deny"` (exit 2 denies); works headless too | session |
| `PreCompact` | Before a compaction (`trigger`: manual or auto) | Stop it (exit 2 or `"decision": "block"`) | embedded engine |
| `SessionEnd` | When the session closes | Observe only, with at most a second | session |

`matcher` applies to the tool events: `PreToolUse`, `PostToolUse`, and `PermissionRequest`. It must match the whole tool name (`^(?:matcher)$`); an empty matcher matches every tool. A PermissionRequest hook sees an escalated command or a `prompt` rule as `tool_name` `Bash` with `tool_input.command`, and an MCP call as its `mcp__<server>__<tool>` name with its arguments. The PreToolUse and PostToolUse hooks see MCP tools by the same names. An `apply_patch` call is `tool_name` `apply_patch` with Codex's `tool_input` `{"command": "<patch>"}`, plus `file_path` and `file_paths`, at PreToolUse, PostToolUse, and PermissionRequest; the matchers `apply_patch`, `Edit`, and `Write` all match it, as in Codex. A PreToolUse `updatedInput` with a new `command` replaces the patch.

The process engine runs no PreToolUse, PermissionRequest, or PreCompact hooks; a session on it shows one notice for each of these events that has a hook ([the capability table](../engine/README.md#what-each-engine-supports)).
<!-- /memoria:section -->

<!-- memoria:section id="running" files="exec.go payload.go hooks.go" -->
## Running a hook

`Runner.Run(ctx, Input)` runs the matching hooks in configuration order (the user file's first) and stops at the first block.

1. The command runs with `/bin/sh -c` in the workspace, in its own process group, with `UAH_HOOK_EVENT` and `UAH_PROJECT_DIR` added to the environment.
2. `Input` arrives as JSON on stdin. Its field names are Claude Code's: `hook_event_name`, `session_id`, `cwd`, `transcript_path`, `prompt`, `tool_name`, `tool_input`, `tool_response`, `stop_hook_active`, `trigger`, `source`, and `reason`, plus uah's `run_id`, `model`, and `effort`.
3. The timeout kills the whole process group. Output past 1 MiB is dropped.
4. Every result goes to the function set with `OnResult`; the session reports it as a `HookRan` event, shown in the TUI's detailed view, while blocks and failures show in both views.

The session runs its hooks on one worker goroutine, in order, so a slow hook never blocks the session loop. PreToolUse hooks run on the coordinator's goroutine, as Claude Code's do, so a slow hook delays the agent up to its timeout.
<!-- /memoria:section -->

<!-- memoria:section id="decisions" files="hooks.go payload.go" -->
## Output and decisions

| Exit | Outcome |
| --- | --- |
| 0 | Continue. Stdout that starts with `{` is read as JSON output; invalid JSON is an error |
| 2 | Block, with stderr as the reason |
| Other, or a timeout | An error: reported and ignored |

`Decision` combines one event's results. The JSON output can set `continue: false` with a `stopReason`, `decision: "block"` with a `reason`, a `systemMessage` to show, and `hookSpecificOutput` with `permissionDecision`, `permissionDecisionReason`, `updatedInput`, or `additionalContext`. For `UserPromptSubmit` and `SessionStart`, plain stdout without JSON is context, as in Claude Code.

An example: a PreToolUse hook that blocks destructive commands before the agent runs them. The configuration:

```toml
[[hooks.PreToolUse]]
matcher = "Bash"
command = ".uah/hooks/guard.sh"
timeout = "5s"
```

The script, `.uah/hooks/guard.sh`:

```sh
#!/bin/sh
# The tool call arrives as JSON on stdin. Exit 2 blocks it, and stderr
# becomes the error the model sees.
input=$(cat)
if printf '%s' "$input" | grep -Eq 'rm -rf /|git push (-f|--force)|git reset --hard'; then
	echo "blocked by .uah/hooks/guard.sh: destructive command" >&2
	exit 2
fi
```

For fixed command prefixes, `[approvals] forbid` in the configuration does the same without a script, and it also covers a command inside a pipeline or a list; this repository's `.uah/config.toml` uses it. A hook suits a check that needs code, such as one on the arguments of an MCP tool.

To add an event: add it to `Events` in `hooks.go` and any payload fields to `Input`, then fire it where it happens with `Runner.Has` and `Runner.Run`. `internal/config` accepts `[[hooks.<Event>]]` for every name in `Events`.
<!-- /memoria:section -->

<!-- memoria:section id="trust" files="trust.go script.go" -->
## Trust

Hooks in the user file run as written. Hooks in a trusted project's `.uah/config.toml` run only after `uah hooks trust` records them in `~/.uah/trusted-hooks.json`. Trust is based on content:

1. Each command is recorded by its SHA-256, so a changed command needs trust again.
2. When the command's first word is a path to a local file (absolute, relative to the workspace, or through a variable such as `"$UAH_PROJECT_DIR"/check.sh`), the entry also records the script's path and SHA-256. An edited script is reported as untrusted ("the script changed") until `uah hooks trust` runs again.
3. Entries written before uah hashed scripts still cover commands that run no script; a command that runs one needs trust again.

An untrusted hook is skipped and reported once per command. The file is written atomically.
<!-- /memoria:section -->

<!-- memoria:section id="tests" files="hooks_test.go trust_test.go" -->
## Tests

`hooks_test.go` pins the exit codes, the stdin payload, PreToolUse decisions, timeouts, and validation. `trust_test.go` pins script hashing, commands without a script, old entries, and the "script changed" report. The session's event handling is tested in `internal/session/hooks_test.go`, and PreToolUse and PermissionRequest in `internal/engine/embedded`.
<!-- /memoria:section -->
