package embedded

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/viktordanov/uah-core/harness/inbox"
	"github.com/viktordanov/uah-core/harness/llm"
	"github.com/viktordanov/uah-core/harness/operation"
	"github.com/viktordanov/uah-core/harness/session"
	"github.com/viktordanov/uah-core/harness/sessionstore"
	"github.com/viktordanov/uah-core/harness/sessionstore/localfile"

	"github.com/viktordanov/uagent/core"
)

// runStore is the session a run records to.
// The store leaves out what the session's rewinds cut (cutStore), writes
// fewer operation records (checkpointStore), and reads the file once for
// the run's start and syncs it in groups (logStore).
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
	w.closers = append(w.closers, closer{close: log.Close, saves: true}) // after the checkpoints' flush
	var early []sessionstore.Item
	if !slices.ContainsFunc(restored.Operations, func(op operation.Operation) bool { return !finalOperation(op.Status) }) {
		first := log.AddObserver(func(_ session.ID, it sessionstore.Item) { early = append(early, it) })
		err = recordInputs(ctx, log, id, messages, req.Effort) // the first append reads the file for the run
		log.RemoveObserver(first)
		if err != nil {
			return runStore{}, err
		}
	}
	cut, err := withCuts(log, w.l.SessionsDir, string(id))
	if err != nil {
		return runStore{}, err
	}

	ck := &checkpointStore{Store: cut}
	w.closers = append(w.closers, closer{close: func() error { return ck.flush(context.WithoutCancel(ctx)) }, saves: true}) // after the coordinator

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

// firstEffort is the effort of the session's first settings among items, a
// fork's inherited ones included; "" when it has none.
func firstEffort(items []sessionstore.Item) llm.ReasoningEffort {
	for _, it := range items {
		in, ok := it.Data.(inbox.Input)
		if !ok || in.Kind != inbox.InputControl {
			continue
		}
		msg, err := in.DecodeControlMessage()
		if settings, ok := msg.Parameters.(inbox.Settings); err == nil && ok {
			return settings.ReasoningEffort
		}
	}

	return ""
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
