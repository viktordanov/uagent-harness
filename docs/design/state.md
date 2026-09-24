# State storage: decision record

Status: built, 2026-09-24. The sidecar (with `source`) and the index (`internal/store`, `<state>/uah.db`) are implemented. As built, the index has no `sessions` table: sessions are folded from the `runs` rows at query time, the same way the file scan folds them, and a test checks the two agree. Listing falls back to the file scan when the index cannot be opened. `/status` draws a 12-week activity heatmap from it, and `uah sessions --search` uses its full-text table.

1. [What is stored today](#what-is-stored-today)
2. [How other harnesses store state](#how-other-harnesses-store-state)
3. [What the harness needs to read](#what-the-harness-needs-to-read)
4. [Options](#options)
5. [Decision](#decision)
6. [Design of the index](#design-of-the-index)
7. [When to build it](#when-to-build-it)

## What is stored today

Everything lives under uagent's state directory (`~/.local/state/unreal-agent`), shared by `uagent` and `uah`:

| Path | Written by | Contents | Role |
| --- | --- | --- | --- |
| `sessions/<id>.session.jsonl` | the runner | Every input, turn, model response, and tool status of a session, append-only | The conversation. The runner replays it on resume. |
| `sessions/operations/<id>/<op>/out`, `err` | the runner | Full output of each background command | Tool output |
| `sessions/<id>.lock` | uagent | An advisory lock while a run is live | Keeps one runner per session |
| `logs/<time>.jsonl` | the runner | A copy of its stdout for each invocation | Runner log |
| `runs/<run-id>/request.json` | uagent | The request sent to the runner | Run record |
| `runs/<run-id>/events.jsonl` | uagent | The runner's stdout, byte for byte | Run record; the harness rebuilds transcripts from these |
| `runs/<run-id>/summary.json` | uagent | Status (`running` until the run ends), settings, statistics, answer | Run record; history lists read these |
| `runs/<run-id>/stderr.log` | uagent | The runner's stderr | Diagnostics |
| `sessions/<id>.uah.json` | uah | Where the session started: `{"source":"tui"}` or `"run"`, or `"subagent"` with its `parent` | Hides `uah run` sessions and subagents from the resume picker, as Codex hides `codex exec` sessions; puts subagents under their parent in `uah sessions` |
| `sessions/<id>.agent.json` | uah (subagents) | A subagent's nickname, role, and spawn overrides | `resume_agent` restores them; see [subagents](subagents.md) |
| `sessions/<id>.compaction.jsonl` | uah (embedded engine) | One line per compaction: the builder items it covers, their SHA-256, and the summary | Replays compaction on resume; see [compaction](compaction.md) |
| `logs/uah-tui.log` | uah | TUI diagnostics | Diagnostics |

There is no database. Listing sessions reads every `summary.json` and the first `request.json` of each session: the cost grows with the number of runs, and there is no search.

## How other harnesses store state

| | Conversation | Metadata and listing | Notes |
| --- | --- | --- | --- |
| Codex | JSONL "rollout" files under `~/.codex/sessions/YYYY/MM/DD/` | A SQLite state database, plus `history.jsonl` for prompts | Files are the transcript; the database makes listing, naming, and archiving fast |
| Claude Code | JSONL files under `~/.claude/projects/<cwd with non-alphanumerics replaced by ->/` | None; the directory name scopes by project | Files are deleted after 30 days by default |
| web-tty | The agents' own files | SQLite for its own sessions (ID, tool, path, model, effort, process IDs) | Re-reads transcripts from the agents' files; stores no transcript itself |

## What the harness needs to read

| Need | How often | Today |
| --- | --- | --- |
| Sessions of the current directory, newest first (`uah resume`, the picker, `--last`) | Every start | Reads every run's summary |
| Search sessions by prompt or answer text | Planned | Not possible |
| Hide scripted sessions from the picker, as Codex does by default | Planned | Not recorded anywhere |
| Names, pins, or archived sessions | Later | Not recorded anywhere |
| One session's transcript | On resume | Reads that session's `events.jsonl` files: fine |
| Totals across sessions (tokens, time) | Occasional | Reads every summary |

## Options

| Option | For | Against |
| --- | --- | --- |
| A. Files only, as today | Nothing new; `uagent` and `uah` share everything; easy to inspect and back up | Listing is O(runs); no search; new metadata needs new files |
| B. SQLite as the source of truth | Fast queries and search | The runner writes files regardless, so there would be two truths; `uagent` would need the database too; a corrupt database loses history |
| C. Files as the source of truth, SQLite as a rebuildable index | Fast listing and search; losing the database only costs a rebuild; `uagent` stays file-only; matches Codex | An index to keep in step; a rebuild path to test |

## Decision

Recommended: **C**. The runner's session files and uagent's run records stay authoritative. uah adds `<state>/uah.db`, an index it can delete and rebuild at any time.

Two rules keep the index honest:

1. Nothing lives only in the database. Metadata the files do not carry today (whether a session was interactive, a user-given name, archived) goes into a small sidecar file, `sessions/<id>.uah.json`, and the index copies it.
2. The index notices runs that `uagent` or another `uah` wrote without it. On start, uah lists `runs/` (names only, which is cheap) and reads just the runs it has not indexed or whose `summary.json` changed since.

## Design of the index

- **Driver:** `modernc.org/sqlite`, pure Go, so builds need no cgo. WAL mode, so several `uah` processes can read while one writes.
- **Tables:**

```sql
CREATE TABLE sessions (
  id              TEXT PRIMARY KEY,
  workspace       TEXT NOT NULL,   -- as recorded
  workspace_key   TEXT NOT NULL,   -- normalized: absolute, cleaned, symlinks resolved (the Codex match)
  provider        TEXT, model TEXT, effort TEXT,
  title           TEXT,            -- the sidecar name, else the first prompt
  interactive     INTEGER,         -- from the sidecar; NULL when unknown (uagent runs)
  archived        INTEGER NOT NULL DEFAULT 0,
  created_at      INTEGER, last_activity INTEGER,
  status          TEXT, runs INTEGER, input_tokens INTEGER, output_tokens INTEGER
);
CREATE INDEX sessions_by_dir ON sessions(workspace_key, last_activity DESC);

CREATE TABLE runs (
  run_id      TEXT PRIMARY KEY,
  session_id  TEXT NOT NULL REFERENCES sessions(id),
  started_at  INTEGER, wall_ms INTEGER, status TEXT, complete INTEGER,
  input_tokens INTEGER, output_tokens INTEGER,
  summary_mtime INTEGER, summary_size INTEGER   -- change detection
);

CREATE VIRTUAL TABLE messages USING fts5(session_id UNINDEXED, run_id UNINDEXED, role, text);
```

- **Writes:** the session updates the index when a run starts and when it ends. Everything else is reconciliation from files.
- **Commands:** `uah index rebuild` drops and rebuilds the database; `uah sessions --search <text>` uses the full-text table.
- **Package:** `internal/store` with `Index` (open, reconcile, query) and `Sidecar` (read and write `sessions/<id>.uah.json` atomically: write a temporary file, fsync, rename).

## When to build it

The file scan is fast enough for hundreds of runs. Build the index when any of these arrive: search, hiding scripted sessions from the picker, session names, or listing that feels slow.
The sidecar can come first and alone, because the interactive flag is needed to match Codex's default of hiding scripted sessions.
