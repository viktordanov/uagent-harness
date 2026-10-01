package embedded

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/unreallabsai/unreal-agent/harness/session"
	"github.com/unreallabsai/unreal-agent/harness/sessionstore"
	"github.com/unreallabsai/unreal-agent/harness/sessionstore/localfile"

	"github.com/viktordanov/uagent/core"
)

// runStore is the session a run records to, and the log that copies its output.
// The store leaves out what the session's rewinds cut (cutStore) and reads
// the file once for the run's start (snapshotStore).
type runStore struct {
	store    sessionstore.Store
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
	cut, err := withCuts(withSnapshot(store, id), w.l.SessionsDir, string(id))
	if err != nil {
		return runStore{}, err
	}
	logFile, err := openDatetimeLog(w.l.LogsDir, time.Now())
	if err != nil {
		return runStore{}, err
	}
	w.closers = append(w.closers, logFile.Close)

	return runStore{store: cut, id: id, restored: restored, log: logFile}, nil
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

// snapshotStore serves the run's reads of its session from one read of the
// file: the usage seed and the coordinator's restore each page through the
// whole history, and localfile reads the whole file for each page. Once the
// run records an item, reads go to the file.
type snapshotStore struct {
	*localfile.Store

	id    session.ID
	mu    sync.Mutex
	items []sessionstore.Item
	read  bool // items holds the history
	stale bool // an item was recorded since
}

func withSnapshot(store *localfile.Store, id session.ID) *snapshotStore {
	s := &snapshotStore{Store: store, id: id}
	store.AddObserver(func(id session.ID, _ sessionstore.Item) {
		if id == s.id {
			s.mu.Lock()
			s.items, s.stale = nil, true
			s.mu.Unlock()
		}
	})

	return s
}

// Items pages as localfile does.
func (s *snapshotStore) Items(ctx context.Context, id session.ID, after sessionstore.Sequence, limit int) (sessionstore.Page, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id != s.id || s.stale || limit <= 0 {
		return s.Store.Items(ctx, id, after, limit) //nolint:wrapcheck // the store's errors pass through
	}
	if !s.read {
		page, err := s.Store.Items(ctx, id, sessionstore.BeforeFirst, math.MaxInt)
		if err != nil {
			return page, err //nolint:wrapcheck // the store's errors pass through
		}
		s.items, s.read = page.Items, true
	}
	start := min(uint64(after), uint64(len(s.items)))
	end := min(start+uint64(limit), uint64(len(s.items)))
	page := sessionstore.Page{Items: append([]sessionstore.Item(nil), s.items[start:end]...), NextAfter: after, More: end < uint64(len(s.items))}
	if n := len(page.Items); n > 0 {
		page.NextAfter = page.Items[n-1].Sequence
	}

	return page, nil
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
