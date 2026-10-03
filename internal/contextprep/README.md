<!-- memoria:section id="overview" files="contract.go prepare.go" -->
# Context preparation

Context preparation gives a new session the facts its first turns would otherwise spend tool calls on. At the start of the session, adapters each write a short block about one part of the session: the environment, the sandbox, the workspace, the agent files, and the harness. uah sends the joined blocks once, as a developer message before the first user message. The model reads a developer message as the harness's, not the user's, so the block is no user turn and no request of its own: it goes with the first user message.

<!-- memoria:export id="summary" -->
Every new session starts with one developer message of prepared context, before the first user message. This includes a subagent's session. The message has the git branch, the status, and the tracked files. It names the loaded instruction files, so the model does not search for more. It also gives the shell's and the OS's traps, the sandbox's limits and the session's private `$TMPDIR`, and how to size the Bash tool's output. The system prompt does not change, and the message stays in the session's history, so the prompt cache holds. Resumed and forked sessions get no new message. Turn it off with `context_preparation = false`, `--no-context-preparation`, or `UAH_CONTEXT_PREPARATION=off`.
<!-- /memoria:export -->

1. [The contract](#the-contract)
2. [The prepared block](#the-prepared-block)
3. [The adapters](#the-adapters)
4. [Tests](#tests)

The engine calls `Prepare` in `internal/engine/embedded/prepare.go`; the [engine README](../engine/README.md#context-preparation) describes when.
<!-- /memoria:section -->

<!-- memoria:section id="contract" files="contract.go" -->
## The contract

`contract.go` holds the shared contract. Each adapter implements `Adapter`: `Name()` is the block's title, and `Prepare(ctx, Facts)` returns the block, or `""` when the adapter has nothing to say. `Facts` is what the engine knows when a session starts:

| Field | Value |
| --- | --- |
| `Workspace` | The session's working directory |
| `InstructionFiles` | The instruction files in the system prompt, in order, as `internal/app` loaded them (`embedded.Config.InstructionFiles`) |
| `Shell` | The shell commands run in: `$SHELL`, or `/bin/sh` |
| `GOOS` | The operating system, as `runtime.GOOS` names it |
| `Sandbox` | The sandbox mode of the permission mode when the session starts (`read-only`, `workspace-write`, or `""` with no sandbox, also when the system has none), its network access, and the session's private temporary directory |
| `Subagent` | True in a subagent's session |

The text must be stable for the session: an adapter reads only the facts and the files, never the clock.

The package is a domain package ([architecture](../../docs/documentation/architecture.md#import-directions), rule 2): it imports no uah package and nothing of the runtime. The engine (`internal/engine/embedded/prepare.go`) fills `Facts` and gives an adapter what only the runtime knows, such as `Harness.MaxOutputLength`. No adapter parses another package's text: the instruction files come as paths, not from the system prompt's headers. `Workspace` runs read-only git through `os/exec`, as the domain package `internal/gitdiff` does.
<!-- /memoria:section -->

<!-- memoria:section id="block" files="prepare.go" -->
## The prepared block

`Prepare(ctx, facts, adapters...)` runs the adapters at the same time and joins their blocks in the adapters' order:

```text
<context_preparation>
uah prepared this when the session started, so you need not look it up again. It describes the session as it began; files and git's state may change as you work.

## environment
Commands run in zsh (/bin/zsh -c) on macOS.
...

## sandbox
...

## workspace
...

## agent files
...

## harness
...
</context_preparation>
```

An adapter with nothing to say gets no section. Each block is cut to 4 KiB (`MaxAdapterBytes`), or to the adapter's own cap when it implements `Limited`. The whole message is cut to 16 KiB (`MaxBytes`). A cut ends at a line break with `(cut: the rest is over N bytes)`. When no adapter has anything to say, there is no message.

The engine sends it as a developer message (`core.RoleDeveloper`), so the TUI, `uah sessions show`, the auto-reviewer, and the agent benchmark tell it from the user's messages by its role: a `core.DeveloperMessage` event, a `developer` input in the session file. The TUI shows it as a one-line notice and `uah sessions show` as one line; the auto-reviewer and the benchmark's turn count leave it out. A session from before the developer role has it as a user message; `IsPrepared` recognizes it by its tag there, and the same places treat it the same way.
<!-- /memoria:section -->

<!-- memoria:section id="adapters" files="environment.go sandbox.go workspace.go agentfiles.go harness.go" -->
## The adapters

The engine registers them in this order:

| Adapter | Block |
| --- | --- |
| `Environment` (`environment`) | The shell that runs commands and the operating system, always, at least one line ("Commands run in bash (/bin/bash -c) on Linux."). Models write POSIX sh and GNU flags whatever the shell, so for a shell that is not POSIX (fish, nushell, xonsh, elvish, PowerShell, cmd, csh) it lists the constructs that break and what to write instead, or says to run one `sh -c '…'`. It also lists zsh's glob and word-splitting gotchas, the limits of macOS `/bin/bash` 3.2, and the flags that differ in the BSD tools on macOS and the BSDs. The shell's family comes from its base name |
| `SandboxNotes` (`sandbox`) | What sandboxed commands may write in the session's sandbox mode, where the session's private `$TMPDIR` is (writable in every mode, read-only included) and what it is for, such as `GOCACHE=$TMPDIR/go-build`. On macOS it adds what Seatbelt blocks that a model does not expect: `ps` and `pgrep` (`lsof` works) and, without network, local sockets such as docker's; in read-only with `/bin/bash`, that bash 3.2 ignores `$TMPDIR` for heredocs. The Bash tool's description already says to escalate network commands from the first try, so the block does not repeat it. Without a sandbox (yolo mode, or no sandbox on the system) the block is empty |
| `Workspace` (`workspace`) | The git branch, `git status --short` (at most 20 lines), and `git ls-files` by top directory with file counts (at most 40 entries). Each git command has 2 seconds. Outside a git repository the block is empty |
| `AgentFiles` (`agent files`) | `Facts.InstructionFiles`, in order, with the statement that these are all of them, so there is no need to search for more AGENTS.md or CLAUDE.md files, and that their `@` lines are expanded in place. The [instructions loader](../instructions/README.md#includes) expands them in the system prompt. Whether to load a skill is left to the model |
| `Harness` (`harness`) | How to use the Bash tool's `max_output_length` (its default, `MaxOutputLength`, which the engine passes from uah-core: 40,000 characters, which this does not change): leave it unset when the whole output is needed, set it only for noisy commands and to what will be read, and narrow a command whose output was cut instead of running it again with a bigger limit |
<!-- /memoria:section -->

<!-- memoria:section id="tests" files="prepare_test.go agentfiles_test.go environment_test.go sandbox_test.go" -->
## Tests

| Test | Pins |
| --- | --- |
| `TestPrepare` | The adapters' order, no section for an empty block, no message when every block is empty, the per-adapter cap and an adapter's own cap, and the total cap |
| `TestWorkspace` | No block outside a repository, and the branch, the status, and the listing inside one |
| `TestHarness` | The guidance names `max_output_length` and its default, and no default when none is given |
| `TestAgentFiles` | The instruction files listed in order and said to be all, and the note when there are none |
| `TestEnvironmentName`, `TestEnvironmentPrepare`, `TestShellClaims` | Each shell family's and OS's lines; the last runs each fish and zsh "fails / use instead" claim in the installed shell and skips a shell that is missing |
| `TestSandboxNotesName`, `TestSandboxNotesPrepare` | The block in each sandbox mode, with and without network and `$TMPDIR`, on macOS and Linux, the bash 3.2 heredoc line only where it applies, and no block without a sandbox |
<!-- /memoria:section -->
