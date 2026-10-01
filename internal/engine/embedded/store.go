package embedded

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/unreallabsai/unreal-agent/harness/inbox"
	"github.com/unreallabsai/unreal-agent/harness/operation"
	"github.com/unreallabsai/unreal-agent/harness/session"
	"github.com/unreallabsai/unreal-agent/harness/sessionstore"
	"github.com/unreallabsai/unreal-agent/harness/sessionstore/localfile"

	"github.com/viktordanov/uagent/core"
)

// runStore is the session a run records to.
// The store leaves out what the session's rewinds cut (cutStore), reads
// the file once for the run's start (snapshotStore), writes fewer
// operation records (checkpointStore), and syncs the file in groups
// (logStore).
type runStore struct {
	store    sessionstore.Store
	id       session.ID
	restored sessionstore.ResumeState
	// early are the items openStore recorded, for the run's observer.
	early []sessionstore.Item
}

// openStore opens the session store and the requested session. The run's
// effort and messages go in the session first, unless an operation is
// still to finish: the coordinator restores them with the session's unread
// inputs (a stopped run's, or a fork's inherited ones) and asks the model
// once, where it would ask at once for those alone and cancel the request
// when the inbox brought the messages. With an operation to finish, the
// messages go through the inbox, so the model is asked once its result is
// in. The run's output goes to runs/<id>/events.jsonl only: the runner's
// copy in logs/<time>.jsonl was byte for byte the same, and nothing read it.
func (w *wiring) openStore(ctx context.Context, req core.Request, messages []core.UserInput) (runStore, error) {
	store, err := localfile.New(w.l.SessionsDir)
	if err != nil {
		return runStore{}, fmt.Errorf("failed to open the session store: %w", err)
	}
	id, restored, err := openSession(ctx, store, req.SessionID)
	if err != nil {
		return runStore{}, err
	}
	log := newLogStore(store, filepath.Join(w.l.SessionsDir, string(id)+".session.jsonl"), id)
	w.closers = append(w.closers, log.Close) // after the checkpoints' flush
	var early []sessionstore.Item
	if !slices.ContainsFunc(restored.Operations, func(op operation.Operation) bool { return !finalOperation(op.Status) }) {
		first := log.AddObserver(func(_ session.ID, it sessionstore.Item) { early = append(early, it) })
		err = recordInputs(ctx, log, id, messages, req.Effort) // the first append reads the file, as the coordinator's first write did
		log.RemoveObserver(first)
		if err != nil {
			return runStore{}, err
		}
	}
	cut, err := withCuts(withSnapshot(log, id), w.l.SessionsDir, string(id))
	if err != nil {
		return runStore{}, err
	}

	ck := &checkpointStore{Store: cut}
	w.closers = append(w.closers, func() error { return ck.flush(context.WithoutCancel(ctx)) }) // after the coordinator

	return runStore{store: ck, id: id, restored: restored, early: early}, nil
}

// recordInputs puts the run's effort and messages in the session, as the
// inbox would.
func recordInputs(ctx context.Context, store sessionstore.Store, id session.ID, messages []core.UserInput, effort string) error {
	control, err := controlInput(inbox.ControlMessage{Mode: inbox.UpdateSettings, Parameters: inbox.Settings{ReasoningEffort: reasoningEffort(effort)}})
	inputs := []inbox.Input{control}
	for _, m := range messages {
		in, merr := messageInput(m)
		inputs, err = append(inputs, in), errors.Join(err, merr)
	}
	for _, in := range inputs {
		if err == nil {
			err = store.AppendInput(ctx, id, in)
		}
	}
	if err != nil {
		return fmt.Errorf("failed to record the run's messages: %w", err)
	}

	return nil
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
	sessionstore.Store

	id    session.ID
	mu    sync.Mutex
	items []sessionstore.Item
	stale bool
}

func withSnapshot(store sessionstore.Store, id session.ID) *snapshotStore {
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
