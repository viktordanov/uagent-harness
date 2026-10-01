package embedded

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"math"
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

	"github.com/viktordanov/uah/internal/compaction"
	"github.com/viktordanov/uah/internal/engine"
)

var _ engine.Forker = (*Engine)(nil)

// Fork creates the session childID from the parent's history as it was
// when the model made the call callID: every item before the turn that
// made it that no rewind cut, written in one go as the runner's session
// store writes them, and the compactions that applied to it. The child's
// first run adds its messages after them, so its first model request
// starts with the items of the parent's request that made the call.
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
	path := filepath.Join(dir, childID+".session.jsonl")
	if err := replay(ctx, store, path, session.ID(childID), items[:cut]); err != nil {
		_ = os.Remove(path)

		return err
	}
	if err := copyCompactions(dir, parentID, childID, at); err != nil {
		return err
	}

	return copySearches(dir, parentID, childID)
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

// allItems reads a session's whole history, in one page: localfile reads
// the whole file for each.
func allItems(ctx context.Context, store sessionstore.Store, id session.ID) ([]sessionstore.Item, error) {
	var items []sessionstore.Item
	after := sessionstore.BeforeFirst
	for {
		page, err := store.Items(ctx, id, after, math.MaxInt)
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

// replay appends the parent's items to the child in one write, in the lines
// the store's Append methods write (localfile's encodeRecord), and reads it
// back to check them. The store resumes an operation from the state its
// first tool-call status recorded, kept current by operation lines the
// copy leaves out, so each is followed by an operation line with the last
// status the parent's items show: the child's run never starts the parent's
// work again. A tool call whose operation had not ended is recorded as
// canceled.
func replay(ctx context.Context, store *localfile.Store, path string, id session.ID, items []sessionstore.Item) error {
	last, saved, now := lastOperations(items), map[operation.ID]bool{}, time.Now().UTC()
	var prev session.TurnID
	var out []byte
	var n sessionstore.Sequence
	for _, item := range items {
		rec := itemRecord{Item: item}
		var ended []operation.Operation
		switch d := item.Data.(type) {
		case inbox.Input, sessionstore.ModelResponse:
		case session.Turn:
			d.PreviousTurnID, prev = prev, d.ID // a rewind may have cut the turn before it
			rec.Item.Data = d
		case sessionstore.ToolCallStatus:
			rec.Operations, d.Operations = slices.Clone(d.Operations), nil
			for i, op := range rec.Operations {
				if !finalOperation(last[op.ID].Status) {
					rec.Operations[i].Status = operation.StatusCanceled
				}
			}
			for _, op := range rec.Operations {
				if l := last[op.ID]; !saved[op.ID] && finalOperation(l.Status) && l.Status != op.Status {
					op.Status, op.State = l.Status, nil // the item has the state; an ended operation's is never read
					ended = append(ended, op)
				}
				saved[op.ID] = true
			}
			rec.Item.Data = d
		default: // a fork of the runner's own: its inherited calls have no results to keep
			continue
		}
		n++
		rec.Item.Sequence, rec.Item.RecordedAt = n, now
		line, err := logLine("item", rec)
		out = append(out, line...)
		for _, op := range ended {
			if err == nil {
				line, err = logLine("operation", struct{ Operation operation.Operation }{op})
				out = append(out, line...)
			}
		}
		if err != nil {
			return fmt.Errorf("failed to copy the parent's history: %w", err)
		}
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err == nil {
		_, err = f.Write(out)
		err = errors.Join(err, f.Sync(), f.Close())
	}
	if err == nil {
		_, err = store.Inspect(ctx, id)
	}
	if err != nil {
		return fmt.Errorf("failed to copy the parent's history: %w", err)
	}

	return nil
}

// logLine is a record of the session file, as localfile's encodeRecord
// writes it.
func logLine(kind string, v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err == nil {
		data, err = json.Marshal(struct {
			Type string         `json:"type"`
			Data jsontext.Value `json:"data"`
		}{kind, data})
	}

	return append(data, '\n'), err
}

// lastOperations are the operations' last states the items record.
func lastOperations(items []sessionstore.Item) map[operation.ID]operation.Operation {
	last := map[operation.ID]operation.Operation{}
	for _, item := range items {
		if s, ok := item.Data.(sessionstore.ToolCallStatus); ok {
			for _, op := range s.Operations {
				last[op.ID] = op
			}
		}
	}

	return last
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
