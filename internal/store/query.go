package store

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/session"
)

// Sessions lists the indexed sessions, most recently active first, as
// session.Sessions does from the files.
func (ix *Index) Sessions(ctx context.Context) ([]session.Info, error) {
	return ix.sessions(ctx, `SELECT session_id, started_ns, wall_ns, status, provider, model, effort, workspace, prompt, tokens FROM runs ORDER BY session_id, started_ns DESC`)
}

// Search lists the sessions whose prompts or answers match query (SQLite
// full-text syntax; plain words match all of them), most recent first.
func (ix *Index) Search(ctx context.Context, query string) ([]session.Info, error) {
	return ix.sessions(ctx, `SELECT session_id, started_ns, wall_ns, status, provider, model, effort, workspace, prompt, tokens FROM runs
		WHERE session_id IN (SELECT session_id FROM messages WHERE messages MATCH ?) ORDER BY session_id, started_ns DESC`, ftsQuery(query))
}

// Activity counts runs per local day for the last days days, keyed by the
// day's date (YYYY-MM-DD).
func (ix *Index) Activity(ctx context.Context, now time.Time, days int) (map[string]int, error) {
	since := now.AddDate(0, 0, -days).UnixNano()
	rows, err := ix.db.QueryContext(ctx, `SELECT started_ns FROM runs WHERE started_ns >= ?`, since)
	if err != nil {
		return nil, fmt.Errorf("failed to read activity: %w", err)
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var ns int64
		if err := rows.Scan(&ns); err != nil {
			return nil, fmt.Errorf("failed to read activity: %w", err)
		}
		counts[time.Unix(0, ns).In(now.Location()).Format(time.DateOnly)]++
	}

	return counts, rows.Err()
}

// sessions folds run rows, grouped by session and newest first, into Infos.
func (ix *Index) sessions(ctx context.Context, query string, args ...any) ([]session.Info, error) {
	rows, err := ix.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to read sessions: %w", err)
	}
	defer rows.Close()
	var infos []session.Info
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.session, &r.started, &r.wall, &r.status, &r.provider, &r.model, &r.effort, &r.workspace, &r.prompt, &r.tokens); err != nil {
			return nil, fmt.Errorf("failed to read sessions: %w", err)
		}
		if len(infos) == 0 || infos[len(infos)-1].ID != r.session {
			infos = append(infos, r.newest())
		}
		r.fold(&infos[len(infos)-1])
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read sessions: %w", err)
	}
	sessionsDir := filepath.Join(ix.stateDir, "sessions")
	for i := range infos {
		if sc, found, err := session.ReadSidecar(sessionsDir, infos[i].ID); err == nil && found {
			infos[i].Source, infos[i].Parent = sc.Source, sc.Parent
		}
	}
	slices.SortStableFunc(infos, func(a, b session.Info) int { return b.LastActivity.Compare(a.LastActivity) })

	return infos, nil
}

type row struct {
	session, status, provider, model, effort, workspace, prompt, tokens string
	started, wall                                                       int64
}

// newest starts an Info from the session's newest run.
func (r row) newest() session.Info {
	return session.Info{
		ID: r.session, Provider: r.provider, Model: r.model, Effort: r.effort,
		Workspace: r.workspace, Status: core.Status(r.status),
	}
}

// fold adds a run (rows arrive newest first) to its session's Info.
func (r row) fold(in *session.Info) {
	started := time.Unix(0, r.started)
	in.Runs++
	in.Started = started
	in.FirstPrompt = r.prompt
	var t core.Tokens
	if json.Unmarshal([]byte(r.tokens), &t) == nil {
		in.Tokens = in.Tokens.Add(t)
	}
	if end := started.Add(time.Duration(r.wall)); end.After(in.LastActivity) {
		in.LastActivity = end
	}
}

// ftsQuery quotes each word, so user text is never FTS syntax.
func ftsQuery(q string) string {
	words := strings.Fields(q)
	for i, w := range words {
		words[i] = `"` + strings.ReplaceAll(w, `"`, `""`) + `"`
	}

	return strings.Join(words, " ")
}

// List is Sessions for a state directory: it opens the index, reconciles
// it, lists, and closes it. When the index cannot be used it scans the files
// instead, so listing never depends on the index.
func List(ctx context.Context, stateDir string) ([]session.Info, error) {
	ix, err := Open(ctx, stateDir)
	if err != nil {
		return session.Sessions(stateDir)
	}
	defer ix.Close()

	return ix.Sessions(ctx)
}

// SearchIn is Search for a state directory.
func SearchIn(ctx context.Context, stateDir, query string) ([]session.Info, error) {
	ix, err := Open(ctx, stateDir)
	if err != nil {
		return nil, err
	}
	defer ix.Close()

	return ix.Search(ctx, query)
}

// ActivityIn is Activity for a state directory.
func ActivityIn(ctx context.Context, stateDir string, now time.Time, days int) (map[string]int, error) {
	ix, err := Open(ctx, stateDir)
	if err != nil {
		return nil, err
	}
	defer ix.Close()

	return ix.Activity(ctx, now, days)
}
