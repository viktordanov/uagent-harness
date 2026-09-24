package session

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/viktordanov/uagent/core"
	"github.com/viktordanov/uagent/harness"
)

// Info summarizes one session from its run records.
type Info struct {
	ID           string
	FirstPrompt  string
	Provider     string
	Model        string
	Effort       string
	Workspace    string
	Runs         int
	Started      time.Time
	LastActivity time.Time
	Status       core.Status // of the newest run; "running" while one is in progress
	Tokens       core.Tokens
	// Source is where the session started (SourceTUI or SourceRun), or ""
	// when it has no sidecar.
	Source string
	// Parent is the spawning session of a subagent.
	Parent string
}

// LoadedRun is one run of a session with its decoded runner events.
type LoadedRun struct {
	Record harness.RunRecord
	Events []core.Event
}

// Sessions lists sessions in stateDir, most recently active first. It reads
// only summaries and requests, never full event files.
func Sessions(stateDir string) ([]Info, error) {
	records, err := harness.New(harness.Config{StateDir: stateDir}).Runs()
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}
	bySession := map[string][]harness.RunRecord{}
	for _, r := range records {
		id := r.Result.Request.SessionID
		bySession[id] = append(bySession[id], r)
	}
	infos := make([]Info, 0, len(bySession))
	sessionsDir := filepath.Join(stateDir, "sessions")
	for id, runs := range bySession {
		info := summarize(id, runs)
		if sc, found, err := ReadSidecar(sessionsDir, id); err == nil && found {
			info.Source, info.Parent = sc.Source, sc.Parent
		}
		infos = append(infos, info)
	}
	slices.SortFunc(infos, func(a, b Info) int { return b.LastActivity.Compare(a.LastActivity) })

	return infos, nil
}

// Find returns the session with id, or found=false.
func Find(stateDir, id string) (info Info, found bool, err error) {
	infos, err := Sessions(stateDir)
	if err != nil {
		return Info{}, false, err
	}
	for _, in := range infos {
		if in.ID == id {
			return in, true, nil
		}
	}

	return Info{}, false, nil
}

// summarize folds a session's runs (newest first) into an Info.
func summarize(id string, runs []harness.RunRecord) Info {
	newest, oldest := runs[0].Result, runs[len(runs)-1]
	info := Info{
		ID: id, Runs: len(runs), Started: oldest.Result.StartedAt,
		Provider: newest.Request.Provider, Model: newest.Request.Model, Effort: newest.Request.Effort,
		Workspace: newest.Request.Workspace, Status: newest.Status,
	}
	for _, r := range runs {
		info.Tokens = info.Tokens.Add(r.Result.Stats.Tokens)
		end := r.Result.StartedAt.Add(r.Result.Wall)
		if end.After(info.LastActivity) {
			info.LastActivity = end
		}
	}
	if req, err := harness.LoadRequest(oldest.Dir); err == nil {
		info.FirstPrompt = firstPrompt(req)
	}

	return info
}

func firstPrompt(req core.Request) string {
	if req.Prompt != "" {
		return req.Prompt
	}
	texts := make([]string, 0, len(req.Messages))
	for _, m := range req.Messages {
		texts = append(texts, m.Text)
	}

	return strings.Join(texts, "\n")
}

// Load reads every run of a session in start order, with its events. The
// runner writes only the items each run appended, so the runs together are
// the whole transcript.
func Load(stateDir, id string) ([]LoadedRun, error) {
	records, err := harness.New(harness.Config{StateDir: stateDir}).Runs()
	if err != nil {
		return nil, fmt.Errorf("failed to load session: %w", err)
	}
	var runs []LoadedRun
	for _, r := range slices.Backward(records) {
		if r.Result.Request.SessionID != id {
			continue
		}
		loaded := LoadedRun{Record: r}
		if err := harness.LoadEvents(r.Dir, func(e core.Event) { loaded.Events = append(loaded.Events, e) }); err != nil {
			return nil, fmt.Errorf("failed to load run %s: %w", r.Result.Request.RunID, err)
		}
		runs = append(runs, loaded)
	}

	return runs, nil
}

// SameDir reports whether two paths name the same directory after making
// them absolute, cleaning them, and resolving symlinks, the way Codex matches
// a session's working directory.
func SameDir(a, b string) bool {
	return normalizeDir(a) == normalizeDir(b)
}

// InDir keeps the sessions whose workspace is dir.
func InDir(infos []Info, dir string) []Info {
	want := normalizeDir(dir)
	var out []Info
	for _, in := range infos {
		if in.Workspace != "" && normalizeDir(in.Workspace) == want {
			out = append(out, in)
		}
	}

	return out
}

func normalizeDir(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}

	return filepath.Clean(abs)
}
