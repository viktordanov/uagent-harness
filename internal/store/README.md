<!-- memoria:section id="overview" files="store.go query.go" -->
# Session index

The store is a SQLite index of uagent's run records at `<state>/uah.db`. It makes session listing, full-text search, and activity counts fast without rereading every run summary. It can be deleted at any time; the next open rebuilds it from the files.

<!-- memoria:export id="summary" -->
A rebuildable SQLite index of the run records makes session listing, full-text search, and activity counts fast. The files stay the source of truth: the index reconciles against them on every open, and listing falls back to a file scan when the index cannot be used.
<!-- /memoria:export -->

The [state storage record](../../docs/design/state.md) explains why the files stay authoritative and how the index differs from the first design.
<!-- /memoria:section -->

<!-- memoria:section id="index" files="store.go query.go store_test.go" -->
## How it works

1. `Open` creates `uah.db` in WAL mode, so several uah processes can share it, and drops and recreates the tables when the schema version changed.
2. `Reconcile` lists `runs/` and compares each `summary.json`'s modification time and size with the indexed values. It reads only runs that are new or changed and deletes rows for runs that are gone. A summary that does not parse yet is read again next time.
3. Each run is one `runs` row (settings, status, timing, tokens, and the prompt). The prompt and the answer also go into the `messages` FTS5 table.
4. Queries fold the rows into one `session.Info` per session, the same way `session.Sessions` folds the files, and add the sidecar's `source` and `parent`.

| Function | Used by |
| --- | --- |
| `List` | `uah sessions`, `uah resume`, the TUI picker, `--last`, shell completion. Falls back to the file scan |
| `SearchIn` | `uah sessions --search`. Each word is quoted, so user text is never FTS syntax |
| `ActivityIn` | `/status`'s 12-week heatmap |

No code writes to the index except `Reconcile`, so runs written by `uagent` or by another uah process appear on the next open. `TestIndexMatchesTheFiles` requires the index and the file scan to return the same sessions.
<!-- /memoria:section -->
