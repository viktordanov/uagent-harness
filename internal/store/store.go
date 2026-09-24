// Package store keeps a rebuildable SQLite index of the run records in the
// state directory, so listing, searching, and activity do not reread every
// summary (docs/design/state.md). The files stay the source of truth: the
// index is reconciled against them on open, and deleting it costs only a
// rebuild.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/viktordanov/uagent/core"
	"github.com/viktordanov/uagent/harness"
	"github.com/viktordanov/uagent/stream"

	_ "modernc.org/sqlite" // the pure-Go SQLite driver

	"github.com/viktordanov/uagent-harness/internal/images"
)

// schemaVersion changes when the tables do; an index with another version is rebuilt.
const schemaVersion = "1"

const schema = `
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS runs (
	run_id        TEXT PRIMARY KEY,
	session_id    TEXT NOT NULL,
	dir           TEXT NOT NULL,
	started_ns    INTEGER NOT NULL,
	wall_ns       INTEGER NOT NULL,
	status        TEXT NOT NULL,
	provider      TEXT NOT NULL,
	model         TEXT NOT NULL,
	effort        TEXT NOT NULL,
	workspace     TEXT NOT NULL,
	prompt        TEXT NOT NULL,
	tokens        TEXT NOT NULL,
	summary_mtime INTEGER NOT NULL,
	summary_size  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS runs_by_session ON runs (session_id, started_ns);
CREATE VIRTUAL TABLE IF NOT EXISTS messages USING fts5 (session_id UNINDEXED, run_id UNINDEXED, text);
`

// Index is an open index. It is safe for one goroutine; several processes
// can share the file.
type Index struct {
	db       *sql.DB
	stateDir string
}

// Open opens (or creates) the index in stateDir and reconciles it with the
// run records.
func Open(ctx context.Context, stateDir string) (*Index, error) {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create the state dir: %w", err)
	}
	dsn := "file:" + filepath.Join(stateDir, "uah.db") + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open the index: %w", err)
	}
	ix := &Index{db: db, stateDir: stateDir}
	if err := ix.migrate(ctx); err != nil {
		_ = db.Close()

		return nil, err
	}
	if err := ix.Reconcile(ctx); err != nil {
		_ = db.Close()

		return nil, err
	}

	return ix, nil
}

// Close closes the index.
func (ix *Index) Close() error { return ix.db.Close() }

// migrate creates the tables, dropping an index of another schema version.
func (ix *Index) migrate(ctx context.Context) error {
	var version string
	err := ix.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'schema'`).Scan(&version)
	if err == nil && version == schemaVersion {
		return nil
	}
	for _, table := range []string{"meta", "runs", "messages"} {
		if _, err := ix.db.ExecContext(ctx, `DROP TABLE IF EXISTS `+table); err != nil {
			return fmt.Errorf("failed to reset the index: %w", err)
		}
	}
	if _, err := ix.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("failed to create the index: %w", err)
	}
	if _, err := ix.db.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES ('schema', ?)`, schemaVersion); err != nil {
		return fmt.Errorf("failed to record the index version: %w", err)
	}

	return nil
}

// Reconcile brings the index in line with the run records: it lists the run
// directories (names and summary stats only) and reads just the runs that
// are new or whose summary changed, then drops runs that are gone.
func (ix *Index) Reconcile(ctx context.Context) error {
	known, err := ix.knownRuns(ctx)
	if err != nil {
		return err
	}
	runsDir := filepath.Join(ix.stateDir, "runs")
	entries, err := os.ReadDir(runsDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("failed to list runs: %w", err)
	}
	tx, err := ix.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to update the index: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	seen := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(runsDir, e.Name())
		info, err := os.Stat(filepath.Join(dir, harness.SummaryFile))
		if err != nil {
			continue // not a run record yet
		}
		seen[e.Name()] = true
		stamp := stat{info.ModTime().UnixNano(), info.Size()}
		if known[e.Name()] == stamp {
			continue
		}
		if err := indexRun(ctx, tx, dir, stamp); err != nil {
			continue // a half-written summary is read again next time
		}
	}
	for runID := range known {
		if !seen[runID] {
			if err := deleteRun(ctx, tx, runID); err != nil {
				return err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to update the index: %w", err)
	}

	return nil
}

type stat struct {
	mtime, size int64
}

func (ix *Index) knownRuns(ctx context.Context) (map[string]stat, error) {
	rows, err := ix.db.QueryContext(ctx, `SELECT run_id, summary_mtime, summary_size FROM runs`)
	if err != nil {
		return nil, fmt.Errorf("failed to read the index: %w", err)
	}
	defer rows.Close()
	known := map[string]stat{}
	for rows.Next() {
		var id string
		var s stat
		if err := rows.Scan(&id, &s.mtime, &s.size); err != nil {
			return nil, fmt.Errorf("failed to read the index: %w", err)
		}
		known[id] = s
	}

	return known, rows.Err()
}

// indexRun reads one run's summary and request and replaces its rows.
func indexRun(ctx context.Context, tx *sql.Tx, dir string, stamp stat) error {
	data, err := os.ReadFile(filepath.Join(dir, harness.SummaryFile))
	if err != nil {
		return fmt.Errorf("failed to read summary: %w", err)
	}
	var dto stream.SummaryDTO
	if err := json.Unmarshal(data, &dto); err != nil {
		return fmt.Errorf("failed to decode summary: %w", err)
	}
	r := stream.SummaryFromDTO(dto)
	req := r.Request
	if saved, err := harness.LoadRequest(dir); err == nil {
		req.Prompt, req.Messages = saved.Prompt, saved.Messages
	}
	tokens, err := json.Marshal(r.Stats.Tokens)
	if err != nil {
		return fmt.Errorf("failed to encode tokens: %w", err)
	}
	runID := filepath.Base(dir)
	if err := deleteRun(ctx, tx, runID); err != nil {
		return err
	}
	prompt := promptText(req)
	if _, err := tx.ExecContext(ctx, `INSERT INTO runs VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		runID, req.SessionID, dir, r.StartedAt.UnixNano(), int64(r.Wall), string(r.Status),
		req.Provider, req.Model, req.Effort, req.Workspace, prompt, string(tokens), stamp.mtime, stamp.size); err != nil {
		return fmt.Errorf("failed to index run %s: %w", runID, err)
	}
	for _, text := range []string{prompt, r.Answer} {
		if strings.TrimSpace(text) == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO messages (session_id, run_id, text) VALUES (?, ?, ?)`, req.SessionID, runID, text); err != nil {
			return fmt.Errorf("failed to index run %s text: %w", runID, err)
		}
	}

	return nil
}

func deleteRun(ctx context.Context, tx *sql.Tx, runID string) error {
	for _, q := range []string{`DELETE FROM runs WHERE run_id = ?`, `DELETE FROM messages WHERE run_id = ?`} {
		if _, err := tx.ExecContext(ctx, q, runID); err != nil {
			return fmt.Errorf("failed to update the index: %w", err)
		}
	}

	return nil
}

// promptText is the run's prompt, or its messages joined, as session
// listings show them.
func promptText(req core.Request) string {
	if req.Prompt != "" {
		return req.Prompt
	}
	texts := make([]string, 0, len(req.Messages))
	for _, m := range req.Messages {
		texts = append(texts, images.Display(m.Text)) // pasted images show as their placeholders
	}

	return strings.Join(texts, "\n")
}
