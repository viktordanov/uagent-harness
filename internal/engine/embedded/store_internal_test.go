package embedded

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/viktordanov/unreal-agent/harness/session"
	"github.com/viktordanov/unreal-agent/harness/sessionstore"
	"github.com/viktordanov/unreal-agent/harness/sessionstore/localfile"
)

// TestLogStore_PagesAsTheFile: the log store's pages and the usage it
// gives are the file's, for any cursor and limit, from one read of the
// file with the items it writes before the first page; once it has served
// a page, an item it writes sends later reads to the file.
func TestLogStore_PagesAsTheFile(t *testing.T) {
	const id = "a5ad5bba-0726-41cd-bf5d-1d5d4f7b12c6"
	dir := t.TempDir()
	b, err := os.ReadFile(filepath.Join("..", "..", "compaction", "evalrun", "testdata", "sessions", id+".session.jsonl"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, id+".session.jsonl"), b, 0o600))
	file, err := localfile.New(dir)
	require.NoError(t, err)
	snap := newLogStore(file, filepath.Join(dir, id+".session.jsonl"), id)
	t.Cleanup(func() { _ = snap.Close() })

	items, err := allItems(t.Context(), file, id)
	require.NoError(t, err)
	require.NoError(t, snap.AppendTurn(t.Context(), id, session.Turn{ID: "before", PreviousTurnID: lastTurn(items)}))
	items, err = allItems(t.Context(), file, id)
	require.NoError(t, err)
	n := sessionstore.Sequence(len(items))
	for _, after := range []sessionstore.Sequence{0, 1, 50, n - 1, n, n + 5} {
		for _, limit := range []int{1, 7, 256, int(n) + 1} {
			want, err := file.Items(t.Context(), id, after, limit)
			require.NoError(t, err)
			got, err := snap.Items(t.Context(), id, after, limit)
			require.NoError(t, err)
			assert.Equal(t, want, got, "after %d, limit %d", after, limit)
		}
	}
	wantUsage, err := lastUsage(t.Context(), file, id)
	require.NoError(t, err)
	gotUsage, err := lastUsage(t.Context(), snap, id)
	require.NoError(t, err)
	assert.Equal(t, wantUsage, gotUsage)
	assert.Positive(t, gotUsage)

	require.NoError(t, snap.AppendTurn(t.Context(), id, session.Turn{ID: "next", PreviousTurnID: "before"}))
	page, err := snap.Items(t.Context(), id, n, 10)
	require.NoError(t, err)
	require.Len(t, page.Items, 1, "the recorded turn")
	assert.Equal(t, session.TurnID("next"), page.Items[0].Data.(session.Turn).ID)
}

func lastTurn(items []sessionstore.Item) session.TurnID {
	var last session.TurnID
	for _, it := range items {
		if t, ok := it.Data.(session.Turn); ok {
			last = t.ID
		}
	}

	return last
}
