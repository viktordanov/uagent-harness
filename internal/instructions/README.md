<!-- memoria:section id="overview" files="instructions.go codex.go" -->
# Instructions

The instructions package finds instruction files the way Codex does (AGENTS.md, checked against `codex-rs/core/src/agents_md.rs`) and builds the runner's system prompt from them. unreal-agent-runner reads no instruction files itself, and a system prompt replaces its default one, so uah keeps base instructions in front: the runner's default text, or the text of `model_instructions_file`. The package also holds a copy of Codex's own prompt, which `uah prompts init` writes as `system-codex.md`.

<!-- memoria:export id="summary" -->
uah finds instruction files the way Codex does: the user's AGENTS.md, then one file per directory from the project root down to the workspace (AGENTS.override.md, else AGENTS.md, else a configured fallback such as CLAUDE.md). They are joined, capped at 32 KiB, and placed after the base instructions (the runner's default host prompt, or the file that Codex's `model_instructions_file` key names), and skills come from Codex's skill folders.
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

1. The user file: `~/.uah/AGENTS.md`, or else `$CODEX_HOME/AGENTS.md` (`~/.codex/AGENTS.md` by default). The first that exists wins.
2. The project root: the nearest ancestor of the workspace that holds one of `project_root_markers` (`.git` by default). `[]` means the workspace only, and so does a workspace with no marker above it.
3. One file per directory from the project root down to the workspace: `AGENTS.override.md`, else `AGENTS.md`, else the first of `project_doc_fallback_filenames` that exists (none by default; `["CLAUDE.md"]` reads Claude Code's files). A fallback name with a path separator is ignored, as in Codex.

Later files are more specific. Empty files are skipped. `ProjectDirs` is exported because skill discovery walks the same directories.
<!-- /memoria:section -->

<!-- memoria:section id="prompt" files="instructions.go codex.go codex_prompt.md" -->
## The system prompt

1. `Assemble` joins the files in order, each under a `## <path>` header, and skips blank files. It stops before the file that would pass `project_doc_max_bytes` (32 KiB by default); a first file larger than the cap is cut.
2. `HostPrompt(base, instructions)` puts the base instructions first, then `ProjectHeader` (a short "Project instructions" preamble), then the files. The base is the text of `model_instructions_file`, or `RunnerHostPrompt` (unreal-agent-runner v0.1.1's default text) when that key is not set. With neither a base nor instructions, it returns "", which leaves the runner's own prompt untouched. With a base and no instructions, it returns the base alone.
3. `internal/app/setup.go` reads `model_instructions_file` (`readModelInstructions`: trimmed, and a missing or empty file is an error, as in Codex) and the files (`loadInstructions`). It sets the result as the session's `SystemPrompt`, which both engines send with every run. It reports the files as `InstructionsLoaded`, shown by `/status` and `uah doctor`. The embedded engine lists them in `/context`, which finds them by the whole `ProjectHeader`, so a heading in the base does not split it.

The runner's context builder always puts its own preamble and the skill list before this prompt. `--no-instructions` or `[instructions] enabled = false` turns discovery off, but a `model_instructions_file` still applies. Subagents get the parent's system prompt, and a role's instructions follow it.

`CodexPrompt` (`codex.go`, `codex_prompt.md`) is Codex's base instructions for gpt-6-astra at rust-v0.156.1, word for word. uah never uses it by default. `uah prompts init` writes it as `system-codex.md`, and `model_instructions_file` can name that file. The [configuration reference](../../docs/configuration.md#codexs-prompt) explains why uah copies this prompt and lists the tools it names that uah does not have.
<!-- /memoria:section -->

<!-- memoria:section id="skills" files="instructions.go" -->
## Skills

Skills are Codex's `<name>/SKILL.md` folders. The embedded engine (`internal/engine/embedded/skills.go`) reads them with the runner's own parser and offers them through the runner's `SkillUse` tool. The folders, most specific first:

1. `.agents/skills` in each directory from the workspace up to the project root.
2. The runner's `<workspace>/.harness/skills`.
3. `~/.uah/skills`.
4. `$CODEX_HOME/skills` (`~/.codex/skills` by default).

A name found in a more specific folder wins. The process engine offers only the runner's `.harness/skills`.
<!-- /memoria:section -->

<!-- memoria:section id="tests" files="instructions_test.go" -->
## Tests

`instructions_test.go` pins discovery (override, fallback, root markers, the user file), assembly with the size cap, the host prompt with and without a base, and the embedded Codex prompt. `internal/app/systemprompt_test.go` pins `model_instructions_file` from Setup to the model request, a subagent's request, and `/context`. `internal/engine/embedded/skills_test.go` pins the skill folders.
<!-- /memoria:section -->
