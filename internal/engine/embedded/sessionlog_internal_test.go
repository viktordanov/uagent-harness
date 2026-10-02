package embedded

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/viktordanov/uah-core/harness/inbox"
	"github.com/viktordanov/uah-core/harness/llm"
	"github.com/viktordanov/uah-core/harness/operation"
	"github.com/viktordanov/uah-core/harness/session"
	"github.com/viktordanov/uah-core/harness/sessionstore"
	"github.com/viktordanov/uah-core/harness/sessionstore/localfile"
)

// rewrite writes a session file's records again through the store, one
// call per record, as the coordinator wrote them.
func rewrite(t *testing.T, store sessionstore.Store, id session.ID, file []byte) {
	t.Helper()
	for line := range bytes.Lines(file) {
		var rec struct {
			Type string         `json:"type"`
			Data jsontext.Value `json:"data"`
		}
		require.NoError(t, json.Unmarshal(line, &rec))
		var err error
		switch rec.Type {
		case "item":
			var it itemRecord
			require.NoError(t, json.Unmarshal(rec.Data, &it))
			switch d := it.Item.Data.(type) {
			case inbox.Input:
				err = store.AppendInput(t.Context(), id, d)
			case session.Turn:
				err = store.AppendTurn(t.Context(), id, d)
			case sessionstore.ModelResponse:
				err = store.AppendModelResponse(t.Context(), id, d)
			case sessionstore.ToolCallStatus:
				d.Operations = it.Operations
				err = store.AppendToolCallStatus(t.Context(), id, d)
			}
		case "operation":
			var op struct{ Operation operation.Operation }
			require.NoError(t, json.Unmarshal(rec.Data, &op))
			err = store.SaveOperation(t.Context(), id, op.Operation)
		}
		require.NoError(t, err)
		require.NotEmpty(t, rec.Type)
	}
}

// TestLogStore_WritesAsLocalfile: a recorded session's records, written
// again through localfile and through the log store at the same times,
// make the same file byte for byte. The log store syncs where needsSync
// says, and at Close; the rest wait.
func TestLogStore_WritesAsLocalfile(t *testing.T) {
	const id = "a5ad5bba-0726-41cd-bf5d-1d5d4f7b12c6"
	b, err := os.ReadFile(filepath.Join("..", "..", "compaction", "evalrun", "testdata", "sessions", id+".session.jsonl"))
	require.NoError(t, err)
	header, records, _ := bytes.Cut(b, []byte{'\n'})
	want, got := t.TempDir(), t.TempDir()
	for _, dir := range []string{want, got} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, id+".session.jsonl"), append(header, '\n'), 0o600))
	}
	store, err := localfile.New(want)
	require.NoError(t, err)
	rewrite(t, store, id, records)
	items, err := allItems(t.Context(), store, id)
	require.NoError(t, err)
	times := make([]time.Time, 0, len(items))
	for _, it := range items {
		times = append(times, it.RecordedAt)
	}

	window := syncWindow
	syncWindow = time.Hour
	t.Cleanup(func() { syncWindow = window })
	store, err = localfile.New(got)
	require.NoError(t, err)
	log := newLogStore(store, filepath.Join(got, id+".session.jsonl"), id)
	log.now = func() time.Time { now := times[0]; times = times[1:]; return now }
	var observed []sessionstore.Item
	log.AddObserver(func(_ session.ID, it sessionstore.Item) { observed = append(observed, it) })
	before := sessionSyncs.Load()
	rewrite(t, log, id, records)
	durable := 0
	for line := range bytes.Lines(records) {
		if needsSync(t, line) {
			durable++
		}
	}
	assert.Equal(t, int64(durable), sessionSyncs.Load()-before, "a sync for each state that is not terminal and each turn's end")
	require.NoError(t, log.Close())

	wantFile, err := os.ReadFile(filepath.Join(want, id+".session.jsonl"))
	require.NoError(t, err)
	gotFile, err := os.ReadFile(filepath.Join(got, id+".session.jsonl"))
	require.NoError(t, err)
	assert.True(t, bytes.Equal(wantFile, gotFile), "the same file")
	assert.Equal(t, items, observed, "the observers get the items as localfile reads them")
	t.Logf("%d syncs for %d records", durable, bytes.Count(records, []byte{'\n'}))
}

// needsSync reports whether the policy syncs after the line: an operation
// state that is not terminal, a status that ends an operation other than
// a command, and a response with no tool call.
func needsSync(t *testing.T, line []byte) bool {
	t.Helper()
	var rec struct {
		Type string `json:"type"`
		Data struct {
			itemRecord
			Operation operation.Operation
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(line, &rec))
	if rec.Type == "operation" {
		return !finalOperation(rec.Data.Operation.Status)
	}
	switch d := rec.Data.Item.Data.(type) {
	case sessionstore.ModelResponse:
		return !slices.ContainsFunc(d.Response.Output, func(o llm.Item) bool { return o.Type == llm.ItemToolCall })
	case sessionstore.ToolCallStatus:
		return slices.ContainsFunc(rec.Data.Operations, func(op operation.Operation) bool { return op.Type != operation.TypeShell && finalOperation(op.Status) })
	}

	return false
}

// TestLogStore_RefusesWhatLocalfileCannotRead: a turn out of order, a
// response for a turn not the session's, and a state for an operation no
// status started are refused and not written; a partial last line, which
// a crash leaves, is dropped before the first write.
func TestLogStore_RefusesWhatLocalfileCannotRead(t *testing.T) {
	dir := t.TempDir()
	store, err := localfile.New(dir)
	require.NoError(t, err)
	_, err = store.Create(t.Context(), "s")
	require.NoError(t, err)
	path := filepath.Join(dir, "s.session.jsonl")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	require.NoError(t, err)
	_, err = f.WriteString(`{"type":"item","data":{"It`)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	log := newLogStore(store, path, "s")
	ctx := t.Context()
	require.NoError(t, log.AppendTurn(ctx, "s", session.Turn{ID: "t1"}))
	require.Error(t, log.AppendTurn(ctx, "s", session.Turn{ID: "t2", PreviousTurnID: "t0"}))
	require.Error(t, log.AppendTurn(ctx, "s", session.Turn{ID: "t1", PreviousTurnID: "t1"}))
	require.Error(t, log.AppendModelResponse(ctx, "s", sessionstore.ModelResponse{TurnID: "t0"}))
	require.Error(t, log.SaveOperation(ctx, "s", operation.Operation{ID: "op", Type: "shell", Version: 3, Status: operation.StatusAwaiting}))
	require.Error(t, log.AppendTurn(ctx, "other", session.Turn{ID: "t2", PreviousTurnID: "t1"}))
	require.NoError(t, log.AppendTurn(ctx, "s", session.Turn{ID: "t2", PreviousTurnID: "t1"}))
	require.NoError(t, log.Close())
	require.ErrorIs(t, log.AppendTurn(ctx, "s", session.Turn{ID: "t3", PreviousTurnID: "t2"}), errLogClosed)

	fresh, err := localfile.New(dir)
	require.NoError(t, err)
	items, err := allItems(ctx, fresh, "s")
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, sessionstore.Sequence(2), items[1].Sequence)
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, byte('\n'), b[len(b)-1])
	assert.NotContains(t, string(b), `{"It{`)
}
