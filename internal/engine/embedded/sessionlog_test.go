package embedded_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/viktordanov/uah-core/harness/session"
	"github.com/viktordanov/uah-core/harness/sessionstore/localfile"

	"github.com/viktordanov/uah/internal/engine/embedded"
	"github.com/viktordanov/uah/testing/fakellm"
)

// TestEmbedded_ResumesAfterACrashAtEachSync: a crash of the system keeps
// the session file as its last sync left it. Cut there, after each sync
// of a run with one command, the session resumes; the command never runs
// again; the model gets its whole output once the exit code was synced,
// and an error for the call before; and no operation is left that the
// harness would take as running. The file is synced at each operation
// state that is not terminal, at the turn's end, and at the run's end,
// not at each record.
func TestEmbedded_ResumesAfterACrashAtEachSync(t *testing.T) {
	syncs := embedded.WatchSyncs(t)
	const cmd = `echo ran >> ran.txt; seq 1 40000; seq 1 40000 | sed s/^/e/ >&2`
	e := newEnv(t, fakellm.Reply{Commands: []string{cmd}}, fakellm.Reply{Text: "done"})
	s, ev := e.open(t, e.embedded(), "")
	_, err := s.Submit("run it")
	require.NoError(t, err)
	ev.finished()
	ev.idle()
	id := s.ID()
	require.NoError(t, s.Close())
	reqs := e.llm.Requests()
	require.Len(t, reqs, 2)
	want := reqs[1].ToolOutputs
	require.Len(t, want, 1)

	path := filepath.Join(e.StateDir, "sessions", id+".session.jsonl")
	full, err := os.ReadFile(path)
	require.NoError(t, err)
	header := int64(strings.IndexByte(string(full), '\n') + 1)
	durable := slices.Compact(append([]int64{header}, syncs(id+".session.jsonl")...))
	var needed []int64 // the ends of the records the policy syncs at
	end := int64(0)
	for line := range strings.Lines(string(full)) {
		end += int64(len(line))
		state := strings.HasPrefix(line, `{"type":"operation"`) && strings.Contains(line, `"Status":"awaiting"`)
		if state || strings.Contains(line, `"Kind":"model_response"`) && !strings.Contains(line, `"Type":"tool_call"`) {
			needed = append(needed, end)
		}
	}
	require.Len(t, needed, 4, "process, its group, read_out, and the answer")
	for _, n := range needed {
		assert.Contains(t, durable, n)
	}
	assert.Equal(t, int64(len(full)), durable[len(durable)-1], "the run's end")
	assert.LessOrEqual(t, len(durable), len(needed)+2, "the start, the syncs needed, and the end")
	t.Logf("%d records, %d syncs", strings.Count(string(full), "\n")-1, len(durable)-1)
	ran := filepath.Join(e.Workspace, "ran.txt")

	for _, size := range durable {
		prefix := string(full[:size])
		t.Run(fmt.Sprint(strings.Count(prefix, "\n"), "_lines"), func(t *testing.T) {
			require.NoError(t, os.WriteFile(path, []byte(prefix), 0o600))
			e.llm = fakellm.New(t, fakellm.Reply{Text: "resumed"})
			s, ev := e.open(t, e.embedded(), id)
			_, err := s.Submit("go on")
			require.NoError(t, err)
			ev.finished()
			ev.idle()
			require.NoError(t, s.Close())

			runs, err := os.ReadFile(ran)
			require.NoError(t, err)
			assert.Equal(t, "ran\n", string(runs), "the command ran once")
			reqs := e.llm.Requests()
			require.NotEmpty(t, reqs)
			got := reqs[len(reqs)-1].ToolOutputs
			switch {
			case strings.Contains(prefix, `"Phase":"read_out"`):
				require.NotEmpty(t, got)
				assert.True(t, want[0] == got[len(got)-1], "the model gets the whole output")
			case strings.Contains(prefix, `"type":"operation"`):
				require.NotEmpty(t, got)
				assert.Contains(t, got[len(got)-1], "shell execution", "the call failed: its outcome is unknown")
			default:
				assert.Empty(t, got, "the call was lost with the model's response")
			}
			file, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Empty(t, liveGroups(t, file), "the harness kills no process group")
			store, err := localfile.New(filepath.Join(e.StateDir, "sessions"))
			require.NoError(t, err)
			resumed, err := store.Resume(t.Context(), session.ID(id))
			require.NoError(t, err)
			assert.Empty(t, resumed.Operations)
		})
	}
}
