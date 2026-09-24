package embedded

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/unreallabsai/unreal-agent/harness/session"
	"github.com/unreallabsai/unreal-agent/harness/sessionstore"
	"github.com/unreallabsai/unreal-agent/harness/sessionstore/localfile"

	"github.com/viktordanov/uagent/core"
)

// runStore is the session a run records to, and the log that copies its output.
type runStore struct {
	store    *localfile.Store
	id       session.ID
	restored sessionstore.ResumeState
	log      *os.File
}

// openStore opens the session store, the requested session, and the
// per-invocation log. The log is added to the closers. A forked session's
// first run puts its messages in the store first (see seedFork).
func (w *wiring) openStore(ctx context.Context, req core.Request, messages []core.UserInput) (runStore, error) {
	store, err := localfile.New(w.l.SessionsDir)
	if err != nil {
		return runStore{}, fmt.Errorf("failed to open the session store: %w", err)
	}
	id, restored, err := openSession(ctx, store, req.SessionID)
	if err != nil {
		return runStore{}, err
	}
	seeded, err := w.e.seedFork(ctx, store, id, messages, req.Effort)
	if err != nil {
		return runStore{}, err
	}
	if seeded {
		if restored, err = store.Resume(ctx, id); err != nil {
			return runStore{}, fmt.Errorf("failed to open session %q: %w", id, err)
		}
	}
	logFile, err := openDatetimeLog(w.l.LogsDir, time.Now())
	if err != nil {
		return runStore{}, err
	}
	w.closers = append(w.closers, logFile.Close)

	return runStore{store: store, id: id, restored: restored, log: logFile}, nil
}

// openSession resumes the session, or creates it when it does not exist.
func openSession(ctx context.Context, store *localfile.Store, requested string) (session.ID, sessionstore.ResumeState, error) {
	id := session.ID(strings.TrimSpace(requested))
	if id == "" {
		id = session.ID(uuid.NewString())
	}
	restored, err := store.Resume(ctx, id)
	if err == nil {
		return id, restored, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", sessionstore.ResumeState{}, fmt.Errorf("failed to open session %q: %w", id, err)
	}
	snapshot, err := store.Create(ctx, id)
	if err != nil {
		return "", sessionstore.ResumeState{}, fmt.Errorf("failed to create session %q: %w", id, err)
	}

	return id, sessionstore.ResumeState{Snapshot: snapshot}, nil
}

// openDatetimeLog opens the runner's per-invocation copy of its output.
func openDatetimeLog(dir string, now time.Time) (*os.File, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create the log directory: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, now.UTC().Format("20060102-150405")+".jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("failed to open the session log: %w", err)
	}

	return f, nil
}
