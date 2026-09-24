<!-- memoria:section id="overview" files="parse.go update.go apply.go diff.go tool.go plain.go" -->
# Patches

This package is Codex's `apply_patch`: the patch format that Codex models are trained to edit files with, its parser, its applier, and the diff of what a patch changed. The embedded engine offers it as a tool (see [the engine](../engine/README.md#the-tool-registry)), and the TUI and `uah sessions show` draw the diff.

<!-- memoria:export id="summary" -->
Models edit files with Codex's `apply_patch` tool: a patch of `*** Add File`, `*** Update File` (with `*** Move to`), and `*** Delete File` sections with `@@` hunks, parsed and applied as Codex does, with its lenient context matching and its messages. The embedded engine applies patches inside the writable roots at once, and asks for any other write as for a Bash escalation; the diff it records shows under the call in the TUI and in `uah sessions show`.
<!-- /memoria:export -->

The code is ported from Codex `rust-v0.156.1` (`codex-rs/apply-patch`, Apache License 2.0), with the attribution in each file.

1. [The format](#the-format)
2. [Applying a patch](#applying-a-patch)
3. [The diff](#the-diff)
4. [The tool](#the-tool)
5. [Tests](#tests)
<!-- /memoria:section -->

<!-- memoria:section id="format" files="parse.go update.go" -->
## The format

```
*** Begin Patch
*** Add File: hello.txt
+Hello world
*** Update File: src/app.py
*** Move to: src/main.py
@@ def greet():
-print("Hi")
+print("Hello, world!")
*** Delete File: obsolete.txt
*** End Patch
```

`Parse` returns one `Hunk` per file section: `Add` with the new contents, `Delete`, or `Update` with its `Chunk`s. A chunk has an optional `@@` context line, its old and new lines, and `*** End of File` when it must match the end of the file. The parser is Codex's streaming parser fed one line at a time, and it is lenient in the same ways:

- Whitespace around the markers is ignored, and a patch wrapped in a `<<'EOF'` heredoc is unwrapped.
- An update's first chunk may start without `@@`.
- A bare empty line in a chunk is an empty context line.

Errors are Codex's, for example `invalid hunk at line 5, Expected update hunk to start with a @@ context marker, got: 'bad'`.
<!-- /memoria:section -->

<!-- memoria:section id="apply" files="apply.go update.go" -->
## Applying a patch

`Compute(cwd, hunks)` works out every change without writing, reading each file as the earlier sections of the same patch leave it. `Write(changes)` writes them in order, and creates missing parent directories. `Summary(changes)` is Codex's output: `Success. Updated the following files:`, then `A`, `M`, and `D` lines.

An update finds each chunk as Codex does (`seek_sequence.rs`). It looks after the `@@` context line and after the previous chunk, and tries four matches in turn: exact, then without trailing whitespace, then without surrounding whitespace, then with typographic dashes, quotes, and spaces made ASCII. The matched lines are replaced by the chunk's new lines, and the file ends with a newline. This is Codex's default mode: line endings become LF, and a context line takes the patch's text.

Relative paths resolve against the working directory. `Paths` lists every path a patch writes, move destinations included, for the sandbox check.
<!-- /memoria:section -->

<!-- memoria:section id="diff" files="diff.go plain.go" -->
## The diff

`Diffs(changes)` turns the changes into `FileDiff`s for display: the op, the path and move path, the added and removed counts, and hunks of `DiffLine`s. Each line has its kind (` `, `+`, or `-`), its old and new line numbers, and its text. An update is diffed with Myers' algorithm, with one line of context around each change, as Codex's TUI shows it. An added or deleted file is one hunk of all its lines.

A diff keeps at most 2,000 lines per file and counts the rest in `Omitted`, so a large new file does not bloat the session file. `Plain` writes diffs as text for `uah sessions show`.
<!-- /memoria:section -->

<!-- memoria:section id="tool" files="tool.go" -->
## The tool

Codex offers `apply_patch` as a freeform (custom) tool with a Lark grammar to the models whose catalog entry has `apply_patch_tool_type`. The runner's Responses adapter sends only function tools, so uah offers Codex's function form: one `input` string, with Codex's instructions and grammar in the description (`Description`, `Parameters`).

Hooks see the call as Codex shows it to them: `tool_name` `apply_patch` and `tool_input` `{"command": "<patch>"}`, plus `file_path` and `file_paths` for Claude Code-style scripts (`HookInput`). A matcher of `apply_patch`, `Edit`, or `Write` matches it (`HookAliases`). A PreToolUse hook's `updatedInput` replaces the patch through its `command` (`FromHookInput`). `Describe` names the files for a tool line.
<!-- /memoria:section -->

<!-- memoria:section id="tests" files="parse_test.go apply_test.go diff_test.go" -->
## Tests

`parse_test.go` and `apply_test.go` port Codex's cases: every op, context and `@@` chunks, `*** End of File`, moves, the fuzzy matches, pure additions, and the error messages. `diff_test.go` pins line numbers, context, hunk breaks, and a large rewrite that stays bounded. The engine's tests apply patches end to end (`internal/engine/embedded/patch_test.go`).
<!-- /memoria:section -->
