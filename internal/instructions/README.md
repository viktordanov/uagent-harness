<!-- memoria:section id="overview" files="instructions.go" -->
# Instructions

The instructions package finds instruction files the way Codex does (AGENTS.md, checked against `codex-rs/core/src/agents_md.rs`) and builds the runner's system prompt from them. unreal-agent-runner reads no instruction files itself, and a system prompt replaces its default one, so uah keeps the runner's default text in front.

<!-- memoria:export id="summary" -->
uah finds instruction files the way Codex does: the user's AGENTS.md, then one file per directory from the project root down to the workspace (AGENTS.override.md, else AGENTS.md, else a configured fallback such as CLAUDE.md). They are joined, capped at 32 KiB, and placed after the runner's default host prompt; skills come from Codex's skill folders.
<!-- /memoria:export -->

The keys are in the [configuration reference](../../docs/configuration.md#instructions-and-skills).

1. [Discovery](#discovery)
2. [The system prompt](#the-system-prompt)
3. [Skills](#skills)
4. [Tests](#tests)
<!-- /memoria:section -->

<!-- memoria:section id="discovery" files="instructions.go" -->
## Discovery

`Discover(workspace, userFiles, options)` lists the files in the order they apply:

1. The user file: `~/.config/uagent/AGENTS.md`, or else `$CODEX_HOME/AGENTS.md` (`~/.codex/AGENTS.md` by default). The first that exists wins.
2. The project root: the nearest ancestor of the workspace that holds one of `project_root_markers` (`.git` by default). `[]` means the workspace only, and so does a workspace with no marker above it.
3. One file per directory from the project root down to the workspace: `AGENTS.override.md`, else `AGENTS.md`, else the first of `project_doc_fallback_filenames` that exists (none by default; `["CLAUDE.md"]` reads Claude Code's files). A fallback name with a path separator is ignored, as in Codex.

Later files are more specific. Empty files are skipped. `ProjectDirs` is exported because skill discovery walks the same directories.
<!-- /memoria:section -->

<!-- memoria:section id="prompt" files="instructions.go" -->
## The system prompt

1. `Assemble` joins the files in order, each under a `## <path>` header, and skips blank files. It stops before the file that would pass `project_doc_max_bytes` (32 KiB by default); a first file larger than the cap is cut.
2. `HostPrompt` puts `RunnerHostPrompt`, unreal-agent-runner v0.1.1's default text, first, then a short "Project instructions" preamble, then the files. With no instructions it returns "", which leaves the runner's own prompt untouched.
3. `internal/app/setup.go` (`loadInstructions`) sets the result as the session's `SystemPrompt`, which both engines send with every run. It reports the files as `InstructionsLoaded`, shown by `/status` and `uah doctor`, and the embedded engine lists them in `/context`.

`--no-instructions` or `[instructions] enabled = false` turns discovery off. Subagents get the parent's host prompt.
<!-- /memoria:section -->

<!-- memoria:section id="skills" files="instructions.go" -->
## Skills

Skills are Codex's `<name>/SKILL.md` folders. The embedded engine (`internal/engine/embedded/skills.go`) reads them with the runner's own parser and offers them through the runner's `SkillUse` tool. The folders, most specific first:

1. `.agents/skills` in each directory from the workspace up to the project root.
2. The runner's `<workspace>/.harness/skills`.
3. `~/.config/uagent/skills`.
4. `$CODEX_HOME/skills` (`~/.codex/skills` by default).

A name found in a more specific folder wins. The process engine offers only the runner's `.harness/skills`.
<!-- /memoria:section -->

<!-- memoria:section id="tests" files="instructions_test.go" -->
## Tests

`instructions_test.go` pins discovery (override, fallback, root markers, the user file), assembly with the size cap, and the host prompt. `internal/engine/embedded/skills_test.go` pins the skill folders.
<!-- /memoria:section -->
