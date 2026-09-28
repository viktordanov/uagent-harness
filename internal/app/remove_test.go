package app_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/harness"

	"github.com/viktordanov/uagent-harness/internal/app"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/store"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

// TestRemoveSession runs a session that spawns a subagent and another
// session beside it, then removes the first: its files, tool output, run
// records, and index rows go with its subagent's, and the other session
// stays. A lock a run holds stops it unless forced.
func TestRemoveSession(t *testing.T) {
	_, in := setupEnv(t)
	t.Setenv("OPENAI_API_KEY", "test-key")
	llm := fakellm.New(t,
		fakellm.Reply{Commands: []string{"echo hi"}, Calls: []fakellm.Call{{Name: "spawn_agent", Args: `{"message":"CHILD-R look around"}`}}},
		fakellm.Reply{From: func(req fakellm.Request) fakellm.Reply {
			out := strings.Join(req.ToolOutputs, "")
			id := strings.Split(strings.Split(out, `"agent_id":"`)[1], `"`)[0]
			return fakellm.Reply{Calls: []fakellm.Call{{Name: "wait_agent", Args: `{"targets":["` + id + `"]}`}}}
		}},
		fakellm.Reply{Text: "done"},
		fakellm.Reply{Text: "other"},
	)
	llm.Route("CHILD-R", fakellm.Reply{Text: "looked"})
	in.Provider, in.Model, in.BaseURL = "openai", "gpt-test", llm.URL
	root := runSession(t, in, "delegate")
	other := runSession(t, in, "unrelated")
	infos, err := session.Sessions(in.StateDir)
	require.NoError(t, err)
	require.Len(t, infos, 3, "the root, its subagent, and the other session")
	runs := map[string]int{}
	for _, info := range infos {
		runs[info.ID] = info.Runs
	}
	sessions := filepath.Join(in.StateDir, "sessions")

	r, err := session.PlanRemoval(in.StateDir, root)
	require.NoError(t, err)

	require.Len(t, r.IDs, 2)
	child := r.IDs[1]
	assert.Equal(t, root, r.IDs[0])
	assert.True(t, strings.HasPrefix(child, session.SubagentIDPrefix), child)
	for _, p := range []string{
		filepath.Join(sessions, root+".session.jsonl"), filepath.Join(sessions, root+".uah.json"), filepath.Join(sessions, root+".lock"),
		filepath.Join(sessions, "operations", root), filepath.Join(sessions, child+".session.jsonl"), filepath.Join(sessions, child+".agent.json"),
	} {
		assert.Contains(t, r.Paths, p)
	}
	for _, p := range r.Paths {
		assert.NotContains(t, p, other, "nothing of the other session")
	}
	assert.Equal(t, runs[root]+runs[child], countUnder(r.Paths, filepath.Join(in.StateDir, "runs")), "the runs of both")

	t.Run("a lock a run holds stops it unless forced", func(t *testing.T) {
		unlock, err := harness.LockSession(in.StateDir, root)
		require.NoError(t, err)
		defer func() { _ = unlock() }()

		_, err = r.Lock(false)
		require.ErrorIs(t, err, harness.ErrSessionBusy)
		release, err := r.Lock(true)
		require.NoError(t, err)
		release()
	})

	release, err := r.Lock(false)
	require.NoError(t, err)
	require.NoError(t, r.Remove())
	release()
	require.NoError(t, store.ForgetIn(context.Background(), in.StateDir, r.IDs))

	for _, p := range r.Paths {
		assert.NoFileExists(t, p)
		assert.NoDirExists(t, p)
	}
	assert.Equal(t, []string{other}, sessionIDs(t, in.StateDir), "the file scan")
	infos, err = store.List(context.Background(), in.StateDir)
	require.NoError(t, err)
	require.Len(t, infos, 1, "the index")
	assert.Equal(t, other, infos[0].ID)
	_, err = session.PlanRemoval(in.StateDir, root)
	require.ErrorIs(t, err, session.ErrNoSession)
}

// runSession runs one message in a new session and returns its ID.
func runSession(t *testing.T, in app.Inputs, text string) string {
	t.Helper()
	res, err := app.Setup(context.Background(), in, io.Discard)
	require.NoError(t, err)
	opts := res.Options
	opts.Source = session.SourceRun
	s, err := session.Open(context.Background(), res.Engine, opts)
	require.NoError(t, err)
	_, err = s.Submit(text)
	require.NoError(t, err)
	waitFinished(t, s)
	require.NoError(t, s.Close())

	return s.ID()
}

func countUnder(paths []string, dir string) int {
	n := 0
	for _, p := range paths {
		if strings.HasPrefix(p, dir+string(os.PathSeparator)) {
			n++
		}
	}

	return n
}
