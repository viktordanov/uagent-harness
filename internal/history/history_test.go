package history_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/internal/history"
)

func newFile(t *testing.T, maxBytes int64) history.File {
	t.Helper()
	f, err := history.New(t.TempDir(), "", &maxBytes)
	require.NoError(t, err)

	return f
}

func texts(t *testing.T, f history.File) []string {
	t.Helper()
	entries, err := f.Load()
	require.NoError(t, err)
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Text)
	}

	return out
}

func TestAppendAndLoadInCodexFormat(t *testing.T) {
	f := newFile(t, 0)
	require.NoError(t, f.Append(history.Entry{SessionID: "s1", TS: 1700000000, Text: "fix the tests"}))
	require.NoError(t, f.Append(history.Entry{SessionID: "s1", TS: 1700000001, Text: "two\nlines"}))

	data, err := os.ReadFile(f.Path)
	require.NoError(t, err)
	assert.Equal(t, `{"session_id":"s1","ts":1700000000,"text":"fix the tests"}`+"\n"+
		`{"session_id":"s1","ts":1700000001,"text":"two\nlines"}`+"\n", string(data))
	assert.Equal(t, []string{"fix the tests", "two\nlines"}, texts(t, f))
	assert.Equal(t, filepath.Join(filepath.Dir(f.Path), "history.jsonl"), f.Path)
}

// TestAppendWritesTheWorkspace: a line carries its session's workspace
// after Codex's fields, and reads back with it; Codex's lines have none.
func TestAppendWritesTheWorkspace(t *testing.T) {
	f := newFile(t, 0)
	require.NoError(t, os.WriteFile(f.Path, []byte(`{"session_id":"c","ts":1,"text":"from codex"}`+"\n"), 0o600))
	require.NoError(t, f.Append(history.Entry{SessionID: "s1", TS: 2, Text: "fix it", Workspace: "/src/app"}))

	data, err := os.ReadFile(f.Path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `{"session_id":"s1","ts":2,"text":"fix it","workspace":"/src/app"}`+"\n")
	entries, err := f.Load()
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Empty(t, entries[0].Workspace, "a line without the field belongs to no folder")
	assert.Equal(t, "/src/app", entries[1].Workspace)
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	entries, err := newFile(t, 0).Load()
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestLoadSkipsLinesThatAreNotEntries(t *testing.T) {
	f := newFile(t, 0)
	require.NoError(t, os.WriteFile(f.Path, []byte("{\"text\":\"one\"}\nnot json\n\n{\"text\":\"two\"}"), 0o600))
	assert.Equal(t, []string{"one", "two"}, texts(t, f))
}

func TestAppendKeepsTheFilePrivate(t *testing.T) {
	f := newFile(t, 0)
	require.NoError(t, f.Append(history.Entry{Text: "a"}))
	info, err := os.Stat(f.Path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	require.NoError(t, os.Chmod(f.Path, 0o644)) //nolint:gosec // the test widens it on purpose
	require.NoError(t, f.Append(history.Entry{Text: "b"}))
	info, err = os.Stat(f.Path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "an append narrows it again, as Codex's does")
}

func TestPersistenceNoneWritesNothingButStillReads(t *testing.T) {
	dir := t.TempDir()
	saving, err := history.New(dir, "save-all", nil)
	require.NoError(t, err)
	require.NoError(t, saving.Append(history.Entry{Text: "kept"}))

	none, err := history.New(dir, "none", nil)
	require.NoError(t, err)
	require.NoError(t, none.Append(history.Entry{Text: "dropped"}))
	assert.Equal(t, []string{"kept"}, texts(t, none))

	_, err = history.New(dir, "save-some", nil)
	require.ErrorContains(t, err, `invalid [history] persistence "save-some"`)
}

func TestDefaultCap(t *testing.T) {
	f, err := history.New(t.TempDir(), "", nil)
	require.NoError(t, err)
	assert.Equal(t, int64(history.DefaultMaxBytes), f.MaxBytes)
	assert.Equal(t, history.SaveAll, f.Persistence)
}

func TestAppendTrimsTheOldestLinesToTheSoftCap(t *testing.T) {
	f := newFile(t, 1000)
	for i := range 40 {
		require.NoError(t, f.Append(history.Entry{SessionID: "s", Text: fmt.Sprintf("prompt %02d", i)}))
	}
	info, err := os.Stat(f.Path)
	require.NoError(t, err)
	assert.LessOrEqual(t, info.Size(), int64(1000))
	got := texts(t, f)
	assert.Equal(t, "prompt 39", got[len(got)-1], "the newest entry stays")
	assert.Greater(t, len(got), 10)
	for i := 1; i < len(got); i++ {
		assert.Less(t, got[i-1], got[i], "the oldest go first, in order")
	}
}

func TestAppendKeepsANewestEntryLargerThanTheCap(t *testing.T) {
	f := newFile(t, 100)
	require.NoError(t, f.Append(history.Entry{Text: "small"}))
	big := strings.Repeat("x", 500)
	require.NoError(t, f.Append(history.Entry{Text: big}))
	assert.Equal(t, []string{big}, texts(t, f))
}

// TestConcurrentAppendsNeverInterleave appends from goroutines that open
// the file each time, as separate uah processes do: every line is whole.
func TestConcurrentAppendsNeverInterleave(t *testing.T) {
	f := newFile(t, 0)
	const writers, each = 8, 25
	var wg sync.WaitGroup
	for w := range writers {
		wg.Go(func() {
			for i := range each {
				assert.NoError(t, f.Append(history.Entry{SessionID: fmt.Sprint(w), Text: fmt.Sprintf("%d-%d %s", w, i, strings.Repeat("y", 3000))}))
			}
		})
	}
	wg.Wait()

	file, err := os.Open(f.Path)
	require.NoError(t, err)
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(nil, 1<<20)
	lines := 0
	for scanner.Scan() {
		var e history.Entry
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &e), "line %d is whole", lines)
		lines++
	}
	assert.Equal(t, writers*each, lines)
}

func TestRecorderWritesInOrder(t *testing.T) {
	f := newFile(t, 0)
	r := history.NewRecorder(f)
	var wg sync.WaitGroup
	for i := range 20 {
		r.Add(history.Entry{Text: fmt.Sprintf("%02d", i)})
		wg.Go(func() { assert.NoError(t, r.Flush()) })
	}
	wg.Wait()
	got := texts(t, f)
	require.Len(t, got, 20)
	for i, text := range got {
		assert.Equal(t, fmt.Sprintf("%02d", i), text)
	}
}
