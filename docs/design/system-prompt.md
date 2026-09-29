# The system prompt

Ledger item 63: uah's default base instructions are Codex's prompt for gpt-6-sol with only the changes uah needs, and Codex's `<environment_context>` block ends the system prompt.

1. [What the model sees](#what-the-model-sees)
2. [The prompt](#the-prompt)
3. [The environment context](#the-environment-context)
4. [Subagents](#subagents)
5. [Decisions](#decisions)
6. [Open](#open)

## What the model sees

Checked against Codex rust-v0.156.1 and unreal-agent v0.1.1.

The runner's context builder (`harness/contextbuilder/builder.go`, `SetSystemPrompt`) sends one `system` message:

1. The runner's preamble (`harness/contextbuilder/prompts/preamble.md`): "You run on Unreal Agent Harness", turns, asynchronous tool calls, the heartbeat, and "ending a turn with nothing running ends the session". uah does not change the runner, so the preamble stays.
2. The skill preamble and the `<available_skills>` block, only when the workspace has skills.
3. uah's `SystemPrompt` (`instructions.HostPrompt`): the base instructions, then `# Project instructions` and the AGENTS.md files, then `<environment_context>`.

Codex sends the model's template in the Responses `instructions` field, and sends AGENTS.md, the environment context, the permissions, and the collaboration mode as separate developer or user messages. uah puts all of it in one system message, in the same order.

The base instructions are, in order of precedence: the file that `model_instructions_file` names, else `instructions.DefaultPrompt`. Before this item the default was the runner's host prompt, about 50 tokens ("You are an AI agent running inside an isolated sandbox container"). The new default is about 3,700 tokens. The runner sends the same prefix with every request, so it is cached after the first one.

## The prompt

`internal/instructions/default_prompt.md` is `codex_prompt.md` (gpt-6-sol's `model_messages.instructions_template` in `codex-rs/models-manager/models.json`, word for word) with the nine hunks of `default_prompt.diff`. A test applies the diff to Codex's text and requires the default exactly, so the prompt cannot drift from this table. Size: 18,998 bytes before, 16,489 after.

Risk **low** means that the change renames or deletes text about a mechanism that uah does not have. **Medium** means that it changes behavior the model was tuned with.

| # | Diff line | Section | Change | Why | Risk |
| --- | --- | --- | --- | --- | --- |
| 1 | `@@ -1,2` | Opening line | "You are Codex, an agent based on GPT-6" becomes "You are uah, a coding agent in the user's terminal" | uah is not Codex, the runner's preamble names the harness, and uah also runs models that are not GPT on openrouter, fireworks, and ollama | low (a judgment call) |
| 2 | `@@ -4,3` | Personality | "As Codex" becomes "As uah" | Follows hunk 1 | low |
| 3 | `@@ -38,3` | Autonomy and persistence | "ask the user for clarification while continuing independent work" becomes "... in your final message" | Without an asynchronous question tool, a question can only go in the final message, which ends the run. Otherwise the model could ask in commentary and keep going, and the user would never get a chance to answer | medium |
| 4 | `@@ -48,3` | Working with the user | The `send_user_message_async` / `request_user_input_async` paragraph is rewritten: ask in the `final` channel; ask early only when most of the work depends on the answer, else finish the independent work first; for optional clarification, proceed with a stated assumption. The guidance on multiple-choice questions and bundling stays word for word | uah has neither tool, and does not want one. The 30-second wait and "Elapsed time is not an answer" depend on the asynchronous tool | medium, the riskiest: it changes when the model stops to ask |
| 5 | `@@ -87,3` | Visualizations | Deletes "Prefer interactive visuals when explaining ..." | uah draws Markdown only, with no interactive surface | low |
| 6 | `@@ -91,3` | Visualizations | The Mermaid and inline-visualization sentence becomes Codex's own terminal text, `TERMINAL_VISUALIZATION_INSTRUCTIONS` in `codex-rs/tui/src/terminal_visualization_instructions.rs`, word for word | uah shows a Mermaid block as code. Codex's TUI appends this text behind the `terminal_visualization_instructions` feature | low |
| 7 | `@@ -98,9` | Rules for getting work done | (a) The two `functions.exec` / `Promise.allSettled` bullets become one: "Batch independent searches and reads as parallel tool calls in one response". (b) `exec_command` / `cmd` becomes `Bash` / `command`. (c) `$UAH_HOME` joins `$CODEX_HOME` | (a) Codex runs gpt-6-sol in `tool_mode = "code_mode_only"`; uah offers plain function tools, which the runner runs in parallel. (b) The runner's tool is `Bash` with `command`. (c) uah's home variable; `$CODEX_HOME` stays, because uah still reads `~/.codex` | low |
| 8 | `@@ -115,3` | Using skills | "listed in the "## Skills" section" becomes "listed in the `<available_skills>` block at the start of these instructions" | Where the runner puts the list | low |
| 9 | `@@ -133,24` | How to use skills; Apps; Plugins | `skills.list` / `skills.read` becomes "call `SkillUse` with its exact name, or read its `SKILL.md`"; the `skill://` sentence and the `# Apps (Connectors)` and `# Plugins` sections are deleted | uah has no orchestrator skills, no `codex_apps` MCP server, no `tool_search`, and no plugins. MCP tools keep their `mcp__server__tool` names, which the model sees in the tool list | low |

Kept on purpose, although uah differs:

- **The `commentary` and `final` channels.** The runner keeps each message's Responses `phase` and sends it back with the history, and real sessions under `~/.uah/sessions` already use both channels.
- **"collapsed after the final answer is shown".** Commentary stays visible in uah, but the instruction that goes with it, a self-contained final answer, is still right.
- **Steering, compaction, and the auto-review wording** match uah: ctrl+enter steers, compaction keeps user messages word for word, and auto mode uses Codex's guardian prompt.
- **"Avoid sleep or wait calls longer than 60 seconds".** Runner calls are asynchronous, but the advice costs nothing.
- **File links** `[app.py](/abs/path/app.py:12)`: uah draws the label with the target dim after it. The environment context gives the model the absolute workspace path the links need.

The runner's preamble uses "turn" for one model response and "session" for what uah calls a run; Codex's prompt uses "turn" for everything up to the final answer. Both agree that a final message with nothing running ends the run. A model that ends a response with calls still running may label that message `final_answer`; uah then draws a second answer later in the same run. This has not appeared in recorded sessions.

## The environment context

`instructions.LocalEnvironment` fills the block, and `Environment.String` renders it as Codex's legacy single environment (`codex-rs/core/src/context/world_state/environment.rs`, and `environment_render_tests.rs` for the expected text):

```xml
<environment_context>
  <cwd>/Users/me/code/proj</cwd>
  <shell>zsh</shell>
  <current_date>2026-09-29</current_date>
  <timezone>Europe/Berlin</timezone>
</environment_context>
```

- **cwd**: the session's workspace, absolute.
- **shell**: the base name of the shell `Bash` runs, `$SHELL` or `/bin/sh` (`app.RealShell`). Codex names its shell type (`zsh`, `bash`, `sh`, `powershell`); the base name is the same for those.
- **current_date** and **timezone**: Codex's `local_time_context` (`core/src/session/turn_context.rs`) takes the local date as `%Y-%m-%d` and the IANA zone from `iana_time_zone`, else the UTC date and `Etc/UTC`. uah reads the zone from `$TZ` (a name, or a path under `zoneinfo/`, with an optional leading `:`), else from the `/etc/localtime` link, and falls back the same way.
- Values are XML-escaped as Codex's `push_xml_escaped_text` does, and an empty value is left out.

Codex's block can also carry `<filesystem>` (the sandbox's permission profile), `<network>`, `<shell_version>` (PowerShell only), and `<subagents>`. uah leaves them out; the `Bash` tool's description already names the sandbox mode and network access.

**Placement.** Codex sends the block as a user message after the AGENTS.md message, when a thread starts, and sends a new block when a value changes, such as the date after midnight (`render_diff`). Appending is cache-friendly for Codex, because the history grows at the end. uah has one system message and no hidden context messages: a user message would appear in the transcript, in `uah sessions`, as a session's first prompt, and in what compaction keeps. So the block goes at the end of the system message, after the AGENTS.md files, which keeps Codex's order.

**When the values are taken.** `app.Setup` takes them once, when the session opens. Every request of the session then sends the same system message, and the prompt cache holds. A session that runs past midnight keeps its first date, where Codex would tell the model the new one. A resume on a later day takes the new date, which changes the system message once; a cache that old has usually expired anyway.

The block follows `model_instructions_file` too, and it stays with `--no-instructions`, as Codex's `include_environment_context` is separate from AGENTS.md. `/context` counts it as the system prompt (`contextusage.splitSystem` cuts it off before splitting the files).

## Subagents

A child gets its parent's system prompt, environment included, then one note from Codex's `multi_agent.role.subagent` text (`instructions.SubagentNote`):

> When you provide a response in the final channel, that content is immediately delivered back to your parent agent.
> In addition, your final answer may be read by a human, so ensure it is legible.

Then a role's instructions follow. The rest of Codex's role text names Codex's v2 tools (`followup_task`, `send_message`), which uah does not offer. A grandchild does not get the note twice. A fork gets no note: it keeps its parent's system prompt byte for byte, so that its first request reuses the parent's prompt cache (`internal/agents/fork_test.go`).

## Decisions

- **The identity is "uah"** (hunks 1 and 2). Dropping them keeps "Codex"; they have no effect on tools.
- **No `request_user_input_async`** (hunks 3 and 4). The owner does not want the tool, so the model asks in its final message.
- **The environment in the system message**, fixed at session start, as above.
- **`uah prompts init`** writes the default as `system.md`, Codex's template as `system-codex.md`, and the runner's host prompt as `system-runner.md`, with the last two keys commented out. `model_instructions_file` naming `system-runner.md` gives back the behavior before this item.
- **License.** Codex is Apache-2.0. The default prompt is a modified copy, so `THIRD_PARTY_NOTICES.md` lists it as modified, and `default_prompt.diff` is the notice of what changed. The notice is a Go comment on `DefaultPrompt`, not prompt text, because the model does not need it.

## Open

- A second `final_answer` in one run, when the model waits on running calls: drawing a `final_answer` as the answer only when it ends the run would remove the case.
- Commentary could fold after the answer, which would make "collapsed after the final answer is shown" true.
- When Codex's template changes, apply `default_prompt.diff` to the new `codex_prompt.md`; the test fails until both files and the diff agree.
