package session

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/viktordanov/uagent/harness"

	"github.com/viktordanov/uah/internal/sessionfile"
)

// sidecarSuffix names a session's sidecar in the sessions directory.
const sidecarSuffix = ".uah.json"

// Unused lists the never-used sessions in stateDir that infos does not
// have: a sidecar, but no run and no item in the session file, such as a
// launch that was stopped before its first message. infos are the sessions
// with runs (Sessions, or the index). Each Info comes from the sidecar alone:
// no runs, no first prompt, last_sequence 0, and its creation time as both
// Started and LastActivity. Resuming one under its ID records its history
// from the first message on.
func Unused(stateDir string, infos []Info) ([]Info, error) {
	sessionsDir := filepath.Join(stateDir, "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}
	known := map[string]bool{}
	for _, in := range infos {
		known[in.ID] = true
	}
	var out []Info
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), sidecarSuffix)
		if !ok || e.IsDir() || known[id] {
			continue
		}
		if has, err := hasItems(sessionsDir, id); err != nil || has {
			continue // a session file with history but no run is not listed, as before
		}
		sc, found, err := ReadSidecar(sessionsDir, id)
		if err != nil || !found {
			continue
		}
		in := Info{ID: id, Workspace: sc.Workspace, FirstPrompt: sc.FirstPrompt, Started: sc.Created, LastActivity: sc.Created}
		in.ApplySidecar(sc)
		out = append(out, in)
	}

	return out, nil
}

// WithUnused adds the never-used sessions (Unused) to infos, most recently
// active first.
func WithUnused(stateDir string, infos []Info) ([]Info, error) {
	unused, err := Unused(stateDir, infos)
	if err != nil || len(unused) == 0 {
		return infos, err
	}
	infos = append(slices.Clone(infos), unused...)
	slices.SortStableFunc(infos, func(a, b Info) int { return b.LastActivity.Compare(a.LastActivity) })

	return infos, nil
}

// Used reports whether the session has history: a run, or an item in its
// session file. A session without either never ran, so starting it under
// its ID loses nothing.
func Used(stateDir, id string) (bool, error) {
	has, err := hasItems(filepath.Join(stateDir, "sessions"), id)
	if err != nil || has {
		return has, err
	}
	records, err := harness.New(harness.Config{StateDir: stateDir}).Runs()
	if err != nil {
		return false, fmt.Errorf("failed to list runs: %w", err)
	}

	return slices.ContainsFunc(records, func(r harness.RunRecord) bool { return r.Result.Request.SessionID == id }), nil
}

// hasItems reports whether the session file has an item; a missing file
// has none.
func hasItems(sessionsDir, id string) (bool, error) {
	_, found, err := sessionfile.Last(filepath.Join(sessionsDir, id+".session.jsonl"))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}

	return found, err
}
