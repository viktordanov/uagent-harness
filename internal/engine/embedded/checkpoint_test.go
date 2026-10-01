package embedded_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/unreallabsai/unreal-agent/harness/session"
	"github.com/unreallabsai/unreal-agent/harness/sessionstore/localfile"

	"github.com/viktordanov/uah/testing/fakellm"
)

// TestEmbedded_ResumesAtEachSkippedPhase: the session file of a run that
// stopped at a shell phase the store leaves out ends at the record before
// it, ready or read_out, and one that stopped while it held the terminal
// state ends at the status. Resumed from there, the command has run once
// and the model gets the whole output, as without the stop.
func TestEmbedded_ResumesAtEachSkippedPhase(t *testing.T) {
	// Both outputs are over the read limit, so the shell reads their tails.
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
	require.Contains(t, want[0], "\n40000\n", "the tail of stdout")
	require.Contains(t, want[0], "e40000", "the tail of stderr")

	path := filepath.Join(e.StateDir, "sessions", id+".session.jsonl")
	full, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := strings.SplitAfter(string(full), "\n")
	var phases []string
	ready, readOut, done, ended := -1, -1, -1, 0
	for i, line := range lines {
		if strings.Contains(line, `"Kind":"tool_call_status"`) {
			if ready < 0 {
				ready = i
			}
			done = i
		}
		if strings.HasPrefix(line, `{"type":"operation"`) && strings.Contains(line, `"Status":"completed"`) {
			assert.Equal(t, done, i-1, "the terminal state follows its status")
			assert.NotContains(t, line, `"State"`, "the status has the state")
			ended++
		}
		if strings.HasPrefix(line, `{"type":"operation"`) && strings.Contains(line, `"Status":"awaiting"`) {
			phase := line[strings.Index(line, `"Phase":"`)+9:]
			phases = append(phases, phase[:strings.IndexByte(phase, '"')])
			if strings.HasPrefix(phase, `read_out"`) {
				readOut = i
			}
		}
	}
	assert.Equal(t, []string{"process", "process", "read_out"}, phases, "only the write-ahead point and the exit code")
	assert.Equal(t, 1, ended)
	require.Positive(t, ready)
	require.Greater(t, readOut, ready)
	require.Greater(t, done, readOut)
	store, err := localfile.New(filepath.Join(e.StateDir, "sessions"))
	require.NoError(t, err)
	resumed, err := store.Resume(t.Context(), session.ID(id))
	require.NoError(t, err)
	assert.Empty(t, resumed.Operations, "the terminal record ends the operation")
	opDirs, err := filepath.Glob(filepath.Join(e.StateDir, "sessions", "operations", "*", "*"))
	require.NoError(t, err)
	require.Len(t, opDirs, 1)
	opDir, ran := opDirs[0], filepath.Join(e.Workspace, "ran.txt")

	for _, tc := range []struct {
		phase string
		last  int    // the session file's last line at the stop
		files string // what the operation's directory has: "", "dir", "out", "all"
	}{
		{"create_directory", ready, ""},
		{"create_out", ready, "dir"},
		{"create_err", ready, "out"},
		{"read_out_tail", readOut, "all"},
		{"read_err", readOut, "all"},
		{"read_err_tail", readOut, "all"},
		{"completed", done, "all"}, // its status written, its terminal record not
	} {
		t.Run(tc.phase, func(t *testing.T) {
			require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines[:tc.last+1], "")), 0o600))
			if tc.files != "all" { // the command has not started
				require.NoError(t, os.RemoveAll(opDir))
				require.NoError(t, os.Remove(ran))
			}
			if tc.files == "dir" || tc.files == "out" {
				require.NoError(t, os.Mkdir(opDir, 0o700))
			}
			if tc.files == "out" {
				require.NoError(t, os.WriteFile(filepath.Join(opDir, "out"), nil, 0o600))
			}

			e.llm = fakellm.New(t, fakellm.Reply{Text: "resumed"})
			s, ev := e.open(t, e.embedded(), id)
			_, err := s.Submit("go on")
			require.NoError(t, err)
			ev.finished()
			ev.idle()

			runs, err := os.ReadFile(ran)
			require.NoError(t, err)
			assert.Equal(t, "ran\n", string(runs), "the command ran once")
			reqs := e.llm.Requests()
			require.NotEmpty(t, reqs)
			got := reqs[len(reqs)-1].ToolOutputs // after "still running" when the model was asked first
			require.NotEmpty(t, got)
			assert.True(t, want[0] == got[len(got)-1], "the model gets the whole output")
		})
	}
}
