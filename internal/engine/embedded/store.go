package embedded

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/unreallabsai/unreal-agent/harness/session"
	"github.com/unreallabsai/unreal-agent/harness/sessionstore"
	"github.com/unreallabsai/unreal-agent/harness/sessionstore/localfile"

	"github.com/viktordanov/uagent/core"
)

// runStore is the session a run records to.
// The store leaves out what the session's rewinds cut (cutStore), reads
// the file once for the run's start (snapshotStore), and writes fewer
// operation records (checkpointStore).
type runStore struct {
	store    sessionstore.Store
	id       session.ID
	restored sessionstore.ResumeState
}

// openStore opens the session store and the requested session. A forked
// session's first run puts its messages in the store first (see seedFork).
// The run's output goes to runs/<id>/events.jsonl only: the runner's copy
// in logs/<time>.jsonl was byte for byte the same, and nothing read it.
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

	return runStore{store: &checkpointStore{Store: cut}, id: id, restored: restored}, nil
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

// snapshotStore serves the usage seed's and the coordinator's restore's
// pages of the session from one read of the file, where localfile reads the
// whole file per page. Once the run records an item, reads go to the file.
type snapshotStore struct {
	*localfile.Store

	id    session.ID
	mu    sync.Mutex
	items []sessionstore.Item
	stale bool
}

func withSnapshot(store *localfile.Store, id session.ID) *snapshotStore {
	s := &snapshotStore{Store: store, id: id}
	store.AddObserver(func(session.ID, sessionstore.Item) { s.mu.Lock(); s.items, s.stale = nil, true; s.mu.Unlock() })

	return s
}

func (s *snapshotStore) Items(ctx context.Context, id session.ID, after sessionstore.Sequence, limit int) (sessionstore.Page, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items == nil && !s.stale && id == s.id {
		page, err := s.Store.Items(ctx, id, sessionstore.BeforeFirst, math.MaxInt)
		if err != nil {
			return page, err //nolint:wrapcheck // the store's errors pass through
		}
		s.items = page.Items
	}
	if s.stale || id != s.id || limit <= 0 {
		return s.Store.Items(ctx, id, after, limit) //nolint:wrapcheck // as above
	}
	rest := s.items[min(after, sessionstore.Sequence(len(s.items))):] // item i has sequence i+1
	n := min(limit, len(rest))

	return sessionstore.Page{Items: append([]sessionstore.Item(nil), rest[:n]...), NextAfter: after + sessionstore.Sequence(n), More: n < len(rest)}, nil
}
