package embedded

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/unreallabsai/unreal-agent/harness/inbox"
	"github.com/unreallabsai/unreal-agent/harness/operation"
	"github.com/unreallabsai/unreal-agent/harness/session"
	"github.com/unreallabsai/unreal-agent/harness/sessionstore"
	"github.com/unreallabsai/unreal-agent/harness/sessionstore/localfile"
)

// replayEach is the fork's replay before it wrote in one go: one Append
// through the store per item, and a SaveOperation with the last status of
// each operation its first status started.
func replayEach(t *testing.T, store sessionstore.Store, id session.ID, items []sessionstore.Item) {
	t.Helper()
	last, saved := lastOperations(items), map[operation.ID]bool{}
	var prev session.TurnID
	for _, item := range items {
		var err error
		switch d := item.Data.(type) {
		case inbox.Input:
			err = store.AppendInput(t.Context(), id, d)
		case session.Turn:
			d.PreviousTurnID, prev = prev, d.ID
			err = store.AppendTurn(t.Context(), id, d)
		case sessionstore.ModelResponse:
			err = store.AppendModelResponse(t.Context(), id, d)
		case sessionstore.ToolCallStatus:
			d.Operations = slices.Clone(d.Operations)
			for i, op := range d.Operations {
				if !finalOperation(last[op.ID].Status) {
					d.Operations[i].Status = operation.StatusCanceled
				}
			}
			err = store.AppendToolCallStatus(t.Context(), id, d)
			for _, op := range d.Operations {
				if l := last[op.ID]; err == nil && !saved[op.ID] && finalOperation(l.Status) && l.Status != op.Status {
					op.Status, op.State = l.Status, nil
					err = store.SaveOperation(t.Context(), id, op)
				}
				saved[op.ID] = true
			}
		default:
			continue
		}
		require.NoError(t, err)
	}
}

// TestReplay_AsAppendedOneByOne: a fork written in one go reads back as the
// one-append-per-item replay did, on a recorded session cut at its end, at
// a turn, and in the middle of a tool call (whose operation is canceled):
// the same items apart from when they were recorded, and the same resume
// state, with no operation to resume: the child's run starts none of the
// parent's work again.
func TestReplay_AsAppendedOneByOne(t *testing.T) {
	const parent = "a5ad5bba-0726-41cd-bf5d-1d5d4f7b12c6"
	dir := t.TempDir()
	b, err := os.ReadFile(filepath.Join("..", "..", "compaction", "evalrun", "testdata", "sessions", parent+".session.jsonl"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, parent+".session.jsonl"), b, 0o600))
	store, err := localfile.New(dir)
	require.NoError(t, err)
	items, err := allItems(t.Context(), store, parent)
	require.NoError(t, err)

	midTool := slices.IndexFunc(items, func(it sessionstore.Item) bool {
		s, ok := it.Data.(sessionstore.ToolCallStatus)

		return ok && len(s.Operations) > 0 && !finalOperation(s.Operations[0].Status)
	}) + 1
	atTurn := slices.IndexFunc(items[len(items)/2:], func(it sessionstore.Item) bool { return it.Kind == sessionstore.ItemTurn }) + len(items)/2
	require.Positive(t, midTool)
	require.Less(t, atTurn, len(items))
	for n, cut := range []int{len(items), atTurn, midTool} {
		before, after := session.ID(fmt.Sprint("before-", n)), session.ID(fmt.Sprint("after-", n))
		for _, id := range []session.ID{before, after} {
			_, err := store.Create(t.Context(), id)
			require.NoError(t, err)
		}
		replayEach(t, store, before, items[:cut])
		require.NoError(t, replay(filepath.Join(dir, string(after)+".session.jsonl"), items[:cut]))

		fresh, err := localfile.New(dir) // no cached write state
		require.NoError(t, err)
		want, got := readBack(t, fresh, before), readBack(t, fresh, after)
		assert.Equal(t, want, got)
		assert.Len(t, got.items, cut)
		assert.Empty(t, got.resume.Operations, "no operation runs again")
		if cut == midTool {
			s, _ := got.items[cut-1].Data.(sessionstore.ToolCallStatus)
			require.NotEmpty(t, s.Operations)
			assert.Equal(t, operation.StatusCanceled, s.Operations[0].Status, "the open operation")
		}
	}
}

type readState struct {
	items  []sessionstore.Item
	resume sessionstore.ResumeState
}

// readBack is the session's items without their recording times and its
// resume state without its ID and creation time.
func readBack(t *testing.T, store sessionstore.Store, id session.ID) readState {
	t.Helper()
	items, err := allItems(t.Context(), store, id)
	require.NoError(t, err)
	for i := range items {
		items[i].RecordedAt = time.Time{}
	}
	resume, err := store.Resume(t.Context(), id)
	require.NoError(t, err)
	resume.Snapshot = sessionstore.Snapshot{}

	return readState{items: items, resume: resume}
}
