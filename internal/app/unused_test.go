package app_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/internal/app"
	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/internal/store"
	"github.com/viktordanov/uah/testing/fakellm"
)

// TestSetup_NeverUsedSession pins the lifecycle of a session that opened
// and never ran, such as a launch stopped before its first message: it has
// a sidecar and no session file. It is found by its ID with no first prompt
// and last_sequence 0, it resumes under that ID and then records its
// history, --session-id takes the ID again while it has none, and a session
// with history is refused and left as it was.
func TestSetup_NeverUsedSession(t *testing.T) {
	e, in := setupEnv(t)
	t.Setenv("OPENAI_API_KEY", "test-key")
	llm := fakellm.New(t, fakellm.Reply{Text: "resumed"}, fakellm.Reply{Text: "reused"})
	in.Provider, in.Model, in.BaseURL = "openai", "gpt-test", llm.URL
	const unused, other = "3c5e8c7a-3f1d-4b2e-9c6a-7d8e9f0a1b2c", "4d5e8c7a-3f1d-4b2e-9c6a-7d8e9f0a1b2c"
	sessions := filepath.Join(e.StateDir, "sessions")
	for _, id := range []string{unused, other} {
		in.NewSessionID = id
		s := openTUISession(t, in)
		require.NoError(t, s.Close())
		assert.FileExists(t, filepath.Join(sessions, id+".uah.json"))
		assert.NoFileExists(t, filepath.Join(sessions, id+".session.jsonl"), "it never ran")
	}
	in.NewSessionID = ""

	info, err := app.FindSession(context.Background(), e.StateDir, unused)
	require.NoError(t, err, "found by its ID")
	assert.Equal(t, [3]any{0, "", uint64(0)}, [3]any{info.Runs, info.FirstPrompt, info.LastSequence})
	assert.Equal(t, e.Workspace, info.Workspace)
	assert.Equal(t, session.SourceTUI, info.Source)
	assert.Equal(t, info.Started, info.LastActivity, "active when it was created")
	listed, err := store.List(context.Background(), e.StateDir)
	require.NoError(t, err)
	assert.Empty(t, listed, "the picker and --last list sessions with runs only")

	// uah resume <id>
	in.SessionRef = unused
	res, err := app.Setup(context.Background(), in, io.Discard)
	require.NoError(t, err)
	assert.True(t, res.Options.Resumed)
	assert.Equal(t, unused, res.Options.ID)
	s, err := session.Open(context.Background(), res.Engine, res.Options)
	require.NoError(t, err)
	_, err = s.Submit("first message")
	require.NoError(t, err)
	waitFinished(t, s)
	require.NoError(t, s.Close())
	info, err = app.FindSession(context.Background(), e.StateDir, unused)
	require.NoError(t, err)
	assert.Equal(t, 1, info.Runs)
	assert.Equal(t, "first message", info.FirstPrompt)
	sc, _, err := session.ReadSidecar(sessions, unused)
	require.NoError(t, err)
	assert.Equal(t, "first message", sc.FirstPrompt, "the first message after the resume is the session's first")
	assert.NotZero(t, sc.LastSequence)
	in.SessionRef = ""

	// --session-id <id> of a session with history is refused, and the
	// session stays as it was.
	transcript := filepath.Join(sessions, unused+".session.jsonl")
	before, err := os.ReadFile(transcript)
	require.NoError(t, err)
	in.NewSessionID = unused
	_, err = app.Setup(context.Background(), in, io.Discard)
	_, isUsage := errors.AsType[*app.UsageError](err)
	require.True(t, isUsage, "%v", err)
	assert.ErrorContains(t, err, "session "+unused+" exists")
	after, err := os.ReadFile(transcript)
	require.NoError(t, err)
	assert.Equal(t, before, after)

	// --session-id <id> of a session that never ran takes the ID again.
	in.NewSessionID = other
	s = openTUISession(t, in)
	assert.Equal(t, other, s.ID())
	_, err = s.Submit("hello again")
	require.NoError(t, err)
	waitFinished(t, s)
	require.NoError(t, s.Close())
	info, err = app.FindSession(context.Background(), e.StateDir, other)
	require.NoError(t, err)
	assert.Equal(t, [2]any{1, "hello again"}, [2]any{info.Runs, info.FirstPrompt})
}

// openTUISession sets up and opens a session as the TUI does, without
// waiting for anything.
func openTUISession(t *testing.T, in app.Inputs) *session.Session {
	t.Helper()
	res, err := app.Setup(context.Background(), in, io.Discard)
	require.NoError(t, err)
	opts := res.Options
	opts.Source, opts.Interactive = session.SourceTUI, true
	s, err := session.Open(context.Background(), res.Engine, opts)
	require.NoError(t, err)

	return s
}
