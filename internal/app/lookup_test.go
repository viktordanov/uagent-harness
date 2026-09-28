package app_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/app"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/sessionfile"
	"github.com/viktordanov/uagent-harness/internal/store"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

// TestSidecarLookup runs a session on the embedded engine and reads its
// sidecar as a reader outside uah would: the workspace, the first message
// cut to 200 characters, and the session file's last item, which moves
// only when a turn ends. A sidecar from before uah kept them gets them when
// the session resumes.
func TestSidecarLookup(t *testing.T) {
	_, in := setupEnv(t)
	t.Setenv("OPENAI_API_KEY", "test-key")
	gate := make(chan struct{})
	llm := fakellm.New(t, fakellm.Reply{Text: "one"}, fakellm.Reply{Text: "two", Gate: gate})
	in.Provider, in.Model, in.BaseURL = "openai", "gpt-test", llm.URL
	res, err := app.Setup(context.Background(), in, io.Discard)
	require.NoError(t, err)
	opts := res.Options
	opts.Source = session.SourceRun
	s, err := session.Open(context.Background(), res.Engine, opts)
	require.NoError(t, err)
	sessions := filepath.Join(in.StateDir, "sessions")
	file := filepath.Join(sessions, s.ID()+".session.jsonl")
	prompt := strings.Repeat("é", 150) + strings.Repeat("x", 100)

	_, err = s.Submit(prompt)
	require.NoError(t, err)
	waitFinished(t, s)

	sc := readSidecar(t, sessions, s.ID())
	last, found, err := sessionfile.Last(file)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, in.Workspace, sc.Workspace)
	assert.Equal(t, prompt[:len(strings.Repeat("é", 150))]+strings.Repeat("x", 50), sc.FirstPrompt, "200 characters")
	assert.Equal(t, last.Sequence, sc.LastSequence)
	assert.True(t, last.RecordedAt.Equal(sc.LastActivity))

	t.Run("a turn that is still running leaves it as it was", func(t *testing.T) {
		_, err := s.Submit("second")
		require.NoError(t, err)
		require.Eventually(t, func() bool {
			now, _, err := sessionfile.Last(file)

			return err == nil && now.Sequence > last.Sequence
		}, 10*time.Second, 10*time.Millisecond)

		assert.Equal(t, last.Sequence, readSidecar(t, sessions, s.ID()).LastSequence)

		close(gate)
		waitFinished(t, s)
		after, _, err := sessionfile.Last(file)
		require.NoError(t, err)
		sc := readSidecar(t, sessions, s.ID())
		assert.Equal(t, after.Sequence, sc.LastSequence, "written when the turn ended")
		assert.Equal(t, prompt[:len(strings.Repeat("é", 150))]+strings.Repeat("x", 50), sc.FirstPrompt, "the first message stays")

		infos, err := store.List(context.Background(), in.StateDir)
		require.NoError(t, err)
		require.Len(t, infos, 1)
		assert.Equal(t, after.Sequence, infos[0].LastSequence, "uah sessions --json has it too")
	})
	require.NoError(t, s.Close())

	t.Run("resuming an older sidecar fills them in", func(t *testing.T) {
		old := `{"source":"run","created":"2026-01-01T00:00:00Z"}` + "\n"
		require.NoError(t, os.WriteFile(filepath.Join(sessions, s.ID()+".uah.json"), []byte(old), 0o600))
		resume := in
		resume.SessionRef = s.ID()[:8]

		res, err := app.Setup(context.Background(), resume, io.Discard)
		require.NoError(t, err)
		r, err := session.Open(context.Background(), res.Engine, res.Options)
		require.NoError(t, err)
		require.NoError(t, r.Close())

		sc := readSidecar(t, sessions, s.ID())
		last, _, err := sessionfile.Last(file)
		require.NoError(t, err)
		assert.Equal(t, session.SourceRun, sc.Source)
		assert.Equal(t, in.Workspace, sc.Workspace)
		assert.True(t, strings.HasPrefix(prompt, sc.FirstPrompt) && len([]rune(sc.FirstPrompt)) == session.FirstPromptMax, sc.FirstPrompt)
		assert.Equal(t, last.Sequence, sc.LastSequence)
	})
}

func readSidecar(t *testing.T, sessionsDir, id string) session.Sidecar {
	t.Helper()
	sc, found, err := session.ReadSidecar(sessionsDir, id)
	require.NoError(t, err)
	require.True(t, found)

	return sc
}
