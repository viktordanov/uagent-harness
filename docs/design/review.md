# `/diff` and `/review`

Ledger item 65: `/diff` shows the workspace's git changes, and `/review` has a read-only reviewer look at a set of changes and list its findings, as Codex's commands do.

1. [What Codex does](#what-codex-does)
2. [The design](#the-design)
3. [Decisions](#decisions)
4. [Open](#open)

## What Codex does

Checked against Codex rust-v0.156.1 (commit b412ff3). Paths are under `codex-rs/`.

### `/diff`

- **Collection** (`tui/src/get_git_diff.rs`). `git rev-parse --is-inside-work-tree` first; outside a work tree the result is "`/diff` — _not inside a git repository_". Then, with `git -c core.hooksPath=/dev/null`, the filesystem monitor off, and every `filter.<driver>.clean` and `.process` emptied through `GIT_CONFIG_*`, it runs two commands side by side: `git diff --no-textconv --no-ext-diff --submodule=short --ignore-submodules=dirty --color` for the tracked files, and `git ls-files --others --exclude-standard` for the untracked ones. Each untracked file then gets `git diff --no-index -- /dev/null <file>` with the same flags. The output is the tracked diff followed by the untracked diffs.
- **What it covers.** Plain `git diff` compares the work tree with the index, so a change that is only staged does not show. Binary files show as git's "Binary files … differ" line. The diff runs in the TUI's directory, so the untracked list is that directory's.
- **Display** (`tui/src/app/event_dispatch.rs:1106-1125`). A full-screen pager titled "D I F F", with git's own ANSI colors converted to styled lines. An empty diff is one italic line, "No changes detected." A failure is `Failed to compute diff: <error>`. Nothing enters the transcript or the conversation.

### `/review`

- **The popup** (`tui/src/chatwidget/review_popups.rs`). "Select a review preset": "Review against a base branch" ("(PR Style)"), "Review uncommitted changes", "Review a commit", and "Custom review instructions". The branch picker lists the local branches (`git for-each-ref --format=%(refname:short) refs/heads`, sorted, the default branch first) with the current branch in its subtitle. The commit picker lists the newest 100 commits (`git log -n 100 --pretty=format:%H%x1f%ct%x1f%s`) by subject, searchable by subject and SHA. `/review <text>` skips the popup and reviews with the text as custom instructions. `/review` is refused while a task runs: "'/review' is disabled while a task is in progress."
- **The request** (`prompts/src/review_request.rs`). One user message per target: "Review the current code changes (staged, unstaged, and untracked files) and provide prioritized findings."; for a base branch, the merge base (`git merge-base HEAD <branch>`, or the branch's upstream when that is ahead) and "Run `git diff <sha>` …", or, without one, a prompt that tells the model how to find it; for a commit, its SHA and subject; custom instructions as typed. A hint says what is reviewed: "current changes", "changes against 'main'", "commit 1a2b3c4: subject".
- **How it runs** (`core/src/session/review.rs`, `core/src/tasks/review.rs`). A separate one-shot sub-agent thread with a fresh history: the parent conversation is not included. Its base instructions are the rubric, `prompts/templates/review/rubric.md`, with no developer instructions. Web search, goals, and multi-agent tools are off, and `approval_policy` is `never`. The sandbox is the parent's, and no tool is removed beyond those. The model is `review_model` when set, else the session's; there is no other default.
- **The answer.** The rubric asks for JSON: `findings` (each with `title`, a Markdown `body`, `confidence_score`, `priority` 0–3, and `code_location` with `absolute_file_path` and `line_range`), `overall_correctness` ("patch is correct" or "patch is incorrect"), `overall_explanation`, and `overall_confidence_score`. There is no structured-output schema; the text is parsed as JSON, else the slice from the first `{` to the last `}`, else it becomes the explanation with no findings. A finding without `priority` fails the parse, although the rubric allows leaving it out.
- **Display.** `>> Code review started: <hint> <<` when it starts; the reviewer's command events show as usual, and its message text is held back. When it ends, `<< Code review finished >>`, then an assistant message with the explanation and the findings (`protocol/src/review_format.rs`): "Full review comments:" and, for each, `- <title> — <path>:<start>-<end>` with the body indented under it. The confidence scores and the verdict are not shown.
- **Into the conversation.** Yes. The review's end records a user message in the parent thread, `prompts/templates/review/exit_success.xml`: a `<user_action>` that says the user ran a review, with the explanation and the findings. An interrupted review records `exit_interrupted.xml`. The assistant message is recorded as well. The next turn's input carries both (`review_history_surfaces_in_parent_session`).

## The design

**`/diff`** (`internal/gitdiff`, `state/review.go`, `render/review.go`). `gitdiff.Collect` runs git read-only in the work tree's top: `git diff HEAD` (the empty tree before the first commit) with `--no-ext-diff --no-textconv`, `core.fsmonitor=false`, and `GIT_OPTIONAL_LOCKS=0`, then `git ls-files --others --exclude-standard -z`. It parses the unified diff into `patch.FileDiff`, the edit tool's display diff, and reads each untracked file as an addition. Binary files (a NUL in the first 8,000 bytes, or git's "Binary files" line), files over 512 KB, symbolic links, and other special files get a note in place of their lines; a file keeps 2,000 diff lines and counts the rest, and after 200 untracked files the rest are counted. The TUI puts the result in the transcript as a `KindDiff` item, drawn as the edit tool's diffs are: `DIFF 3 files (+12 -4)`, then each file with its counts and lines. A clean tree says "No changes detected.", and outside a repository "/diff — not inside a git repository". It never reaches the agent.

**The menu.** `/review ` opens Codex's presets in the command menu: `branch`, `uncommitted`, `commit`, and an entry that explains custom instructions. `/review branch ` lists the local branches (default first, the checked-out one marked), and `/review commit ` the newest 100 commits by subject with their short SHA; both filter as you type. The lists are read once per review (`EffLoadReviewTargets`). `/review <text>` is custom instructions. `/review` waits until the agent is idle, as in Codex.

**The run.** `Session.Review(ctx, target)` (`internal/session/review.go`) reports `ReviewStarted`, builds Codex's prompt for the target (`codereview.Prompt`, which finds the merge base), and hands it to the engine's reviewer, `agents.Manager.Review` (`internal/agents/review.go`). The reviewer is a fresh session on the same engine:

- the parent's settings now, in read only mode, with Codex's rubric and the parent's environment context as the system prompt and `review_model` (default: the session's model) as the model;
- the tools `Bash` and `ViewImage` only, through the engine's scope: no `apply_patch`, no MCP tools, no hosted web search (Codex turns search off for a review; the hosted tool and the recorded searches honor the scope), and no agent tools (it is a child, so it is never offered them);
- `engine.Scope.NeverAsk`: every action that would ask is declined before the auto-reviewer, as Codex's `approval_policy = never`;
- a sidecar with the parent as its parent, so `uah sessions` lists it; it is not one of the agent's subagents, so `/agents` and the agent tools never see it.

Its tool events come back as `ReviewActivity`, and its last message is parsed with Codex's fallbacks (`codereview.Parse`). `ReviewFinished` carries the findings, `Interrupted`, or the error. Esc esc, `/stop`, and closing the session stop it.

**The findings.** A `KindReview` item: while it runs, `REVIEW changes against 'main'  42s` and the reviewer's latest command; then the verdict line (`2 findings · patch is incorrect · 1m 12s`), the explanation, and each finding with its priority (`P1`, in the error color for P0 and P1), title, place relative to the workspace, and body as Markdown.

**Into the conversation.** As in Codex, the main agent gets the review: Codex's `exit_success.xml` (or `exit_interrupted.xml`) is held and goes out with your next message, as `Session.Inject` does; it starts no run. The transcript shows that message as a note, "the review went to the main agent with this message". A review that failed sends nothing.

The prompts in `internal/codereview/prompts` are Codex's, verbatim (Apache-2.0, see `THIRD_PARTY_NOTICES.md`).

## Decisions

- **`/diff` covers staged changes too.** Codex's `git diff` leaves out a change that is only staged; `git diff HEAD` shows the work tree against the last commit, which is what the reviewer's prompt calls "staged, unstaged, and untracked". Untracked files come from the work tree's top, not the TUI's directory.
- **The diff is a transcript item, not a pager.** uah has no full-screen pager; the transcript already scrolls, selects, and copies, and the edit tool's diff renderer gives word-level tints. It is marked as `/diff`'s and never sent.
- **Untracked files are read in Go**, not with one `git diff --no-index` per file: the same result with one process, and the binary and size checks happen before reading a whole file.
- **The popup is the command menu.** The menu already completes `/model` values and filters as you type; the three pickers become three argument lists. `/review branch main` and `/review commit <sha>` are also typed forms.
- **The reviewer is read-only.** Codex keeps the parent's sandbox and relies on `approval_policy = never`; uah also sets the read-only sandbox and removes `apply_patch`, since a review never edits. Command rules still apply: an `allow` rule runs a command outside the sandbox, as it does for any session.
- **The reviewer's system prompt is the rubric in place of the host prompt, then the parent's `<environment_context>`**, so uah's default prompt, the subagent note, and AGENTS.md are not in it, as in Codex, whose review thread gets the rubric, the environment context, and the prompt. The runner's own lines about turns and asynchronous tool calls stay before it, since the engine adds them to every request.
- **A finding without a priority parses.** The rubric allows it; Codex's parser does not, and falls back to the raw text.
- **The main agent gets the user message only.** Codex also records the rendered review as an assistant message; uah can hold user messages for the next run (`Inject`) but not write an assistant turn into the runner's history, and the user message already carries the whole review.
- **`review_model` mirrors Codex's key**, with the session's effort. It is a top-level key, apart from `[review]`, which configures the auto-reviewer.

## Open

- Codex's app server can deliver a review detached; uah has no `uah review` command, since `/review` covers the use.
- Codex empties git's filter drivers before `/diff`; uah passes `--no-textconv --no-ext-diff` but does not override `filter.<driver>.clean`.
