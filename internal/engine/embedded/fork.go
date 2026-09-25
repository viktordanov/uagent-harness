package embedded

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/unreallabsai/unreal-agent/harness/inbox"
	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/operation"
	"github.com/unreallabsai/unreal-agent/harness/session"
	"github.com/unreallabsai/unreal-agent/harness/sessionstore"
	"github.com/unreallabsai/unreal-agent/harness/sessionstore/localfile"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/compaction"
	"github.com/viktordanov/uagent-harness/internal/engine"
)

var _ engine.Forker = (*Engine)(nil)

// Fork creates the session childID from the parent's history as it was
// when the model made the call callID: every item before the turn that
// made it that no rewind cut, replayed through the runner's session store,
// and the compactions that applied to it. The child's first run adds its messages
// after them, so its first model request starts with the items of the
// parent's request that made the call.
//
// The runner's own Store.Fork (v0.1.1) is not used: it drops the operation
// snapshots of inherited tool calls, so their results would be missing from
// the child's context.
func (e *Engine) Fork(ctx context.Context, parentID, childID, callID string) error {
	dir := e.sessionsDir()
	store, err := localfile.New(dir)
	if err != nil {
		return fmt.Errorf("failed to open the session store: %w", err)
	}
	parent, err := withCuts(store, dir, parentID)
	if err != nil {
		return err
	}
	items, err := allItems(ctx, parent, session.ID(parentID))
	if err != nil {
		return err
	}
	cut, at, err := forkPoint(items, callID)
	if err != nil {
		return err
	}
	if _, err := store.Create(ctx, session.ID(childID)); err != nil {
		return fmt.Errorf("failed to create session %q: %w", childID, err)
	}
	if err := replay(ctx, store, session.ID(childID), items[:cut]); err != nil {
		_ = os.Remove(filepath.Join(dir, childID+".session.jsonl"))

		return err
	}
	if err := copyCompactions(dir, parentID, childID, at); err != nil {
		return err
	}
	e.forks.Store(childID, true)

	return nil
}

// SetCacheKey makes the session's model requests use key as their prompt
// cache key.
func (e *Engine) SetCacheKey(sessionID, key string) {
	if key == "" || key == sessionID {
		e.cacheKeys.Delete(sessionID)

		return
	}
	e.cacheKeys.Store(sessionID, key)
}

// cacheKey is the session's prompt cache key, "" for its ID.
func (e *Engine) cacheKey(sessionID string) string {
	k, _ := e.cacheKeys.Load(sessionID)
	s, _ := k.(string)

	return s
}

func (e *Engine) sessionsDir() string { return filepath.Join(e.cfg.StateDir, "sessions") }

// allItems reads a session's whole history.
func allItems(ctx context.Context, store sessionstore.Store, id session.ID) ([]sessionstore.Item, error) {
	var items []sessionstore.Item
	after := sessionstore.BeforeFirst
	for {
		page, err := store.Items(ctx, id, after, 512)
		if err != nil {
			return nil, fmt.Errorf("failed to read session %q: %w", id, err)
		}
		items = append(items, page.Items...)
		if !page.More {
			return items, nil
		}
		after = page.NextAfter
	}
}

// forkPoint finds the turn whose model response made the call: the fork
// keeps the items before that turn, which are the input of the request
// that made the call. at is when the response was recorded.
func forkPoint(items []sessionstore.Item, callID string) (cut int, at time.Time, err error) {
	var turn session.TurnID
	for _, item := range items {
		r, ok := item.Data.(sessionstore.ModelResponse)
		if ok && slices.ContainsFunc(r.Response.Output, func(o llm.Item) bool {
			c, ok := o.Data.(llm.ToolCall)
			return ok && c.CallID == callID
		}) {
			turn, at = r.TurnID, item.RecordedAt

			break
		}
	}
	for i, item := range items {
		if t, ok := item.Data.(session.Turn); ok && turn != "" && t.ID == turn {
			return i, at, nil
		}
	}

	return 0, time.Time{}, fmt.Errorf("the call %q is not in the parent's history", callID)
}

// replay appends the parent's items to the child in order. A tool call
// whose operation had not ended is recorded as canceled for the child, so
// the child's run never starts the parent's work again.
func replay(ctx context.Context, store sessionstore.Store, id session.ID, items []sessionstore.Item) error {
	open := unfinished(items)
	var last session.TurnID
	for _, item := range items {
		var err error
		switch d := item.Data.(type) {
		case inbox.Input:
			err = store.AppendInput(ctx, id, d)
		case session.Turn:
			d.PreviousTurnID, last = last, d.ID // a rewind may have cut the turn before it
			err = store.AppendTurn(ctx, id, d)
		case sessionstore.ModelResponse:
			err = store.AppendModelResponse(ctx, id, d)
		case sessionstore.ToolCallStatus:
			d.Operations = slices.Clone(d.Operations)
			for i, op := range d.Operations {
				if open[op.ID] {
					d.Operations[i].Status = operation.StatusCanceled
				}
			}
			err = store.AppendToolCallStatus(ctx, id, d)
		default: // a fork of the runner's own: its inherited calls have no results to keep
			continue
		}
		if err != nil {
			return fmt.Errorf("failed to copy the parent's history: %w", err)
		}
	}

	return nil
}

// unfinished are the operations whose last recorded state is not final.
func unfinished(items []sessionstore.Item) map[operation.ID]bool {
	open := map[operation.ID]bool{}
	for _, item := range items {
		if s, ok := item.Data.(sessionstore.ToolCallStatus); ok {
			for _, op := range s.Operations {
				open[op.ID] = !finalOperation(op.Status)
			}
		}
	}

	return open
}

func finalOperation(s operation.Status) bool {
	return s == operation.StatusCompleted || s == operation.StatusFailed || s == operation.StatusCanceled
}

// copyCompactions gives the child the parent's compactions recorded before
// at, so the child's requests are compacted as the parent's request was.
func copyCompactions(dir, parentID, childID string, at time.Time) error {
	records, _, err := compaction.OpenLog(dir, parentID).Records()
	if err != nil {
		return err
	}
	child := compaction.OpenLog(dir, childID)
	for _, rec := range records {
		if rec.At.After(at) {
			break
		}
		if err := child.Append(rec); err != nil {
			return err
		}
	}

	return nil
}

// seedFork gives a forked session's first run its messages and effort in
// the store, after the parent's history, before the coordinator restores
// it. Restoring counts the inherited inputs as undelivered, so the
// coordinator asks the model at once: the messages must already be there.
// The inbox then drops them as seen.
func (e *Engine) seedFork(ctx context.Context, store sessionstore.Store, id session.ID, messages []core.UserInput, effort string) (bool, error) {
	if _, ok := e.forks.LoadAndDelete(string(id)); !ok {
		return false, nil
	}
	control, err := controlInput(inbox.ControlMessage{Mode: inbox.UpdateSettings, Parameters: inbox.Settings{ReasoningEffort: reasoningEffort(effort)}})
	if err != nil {
		return false, err
	}
	inputs := []inbox.Input{control}
	for _, m := range messages {
		in, err := messageInput(m)
		if err != nil {
			return false, err
		}
		inputs = append(inputs, in)
	}
	for _, in := range inputs {
		if err := store.AppendInput(ctx, id, in); err != nil {
			return false, fmt.Errorf("failed to start the forked session: %w", err)
		}
	}

	return true, nil
}
