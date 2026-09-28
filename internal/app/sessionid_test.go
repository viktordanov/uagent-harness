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

	"github.com/viktordanov/uagent-harness/internal/app"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

// TestSetup_NewSessionID starts a session with the ID --session-id gives,
// as Claude Code's flag, runs it on the embedded engine, and refuses the ID
// once it exists, an ID that is not a UUID, and resuming at the same time.
func TestSetup_NewSessionID(t *testing.T) {
	_, in := setupEnv(t)
	t.Setenv("OPENAI_API_KEY", "test-key")
	llm := fakellm.New(t, fakellm.Reply{Text: "hi"})
	in.Provider, in.Model, in.BaseURL = "openai", "gpt-test", llm.URL
	const id = "0b5e8c7a-3f1d-4b2e-9c6a-7d8e9f0a1b2c"
	in.NewSessionID = id

	res, err := app.Setup(context.Background(), in, io.Discard)
	require.NoError(t, err)
	assert.Equal(t, id, res.Options.ID)
	assert.False(t, res.Options.Resumed, "--session-id never resumes")
	opts := res.Options
	opts.Source = session.SourceRun
	s, err := session.Open(context.Background(), res.Engine, opts)
	require.NoError(t, err)
	_, err = s.Submit("hello")
	require.NoError(t, err)
	waitFinished(t, s)
	require.NoError(t, s.Close())

	assert.FileExists(t, filepath.Join(in.StateDir, "sessions", id+".session.jsonl"), "the runner's session has the ID")
	assert.Equal(t, []string{id}, sessionIDs(t, in.StateDir))

	for name, change := range map[string]func(*app.Inputs){
		"an ID that exists": func(*app.Inputs) {},
		"an ID with a sidecar only": func(in *app.Inputs) {
			in.NewSessionID = "1b5e8c7a-3f1d-4b2e-9c6a-7d8e9f0a1b2c"
			require.NoError(t, os.WriteFile(filepath.Join(in.StateDir, "sessions", in.NewSessionID+".uah.json"), []byte("{}\n"), 0o600))
		},
		"not a UUID":       func(in *app.Inputs) { in.NewSessionID = "session-1" },
		"a UUID in braces": func(in *app.Inputs) { in.NewSessionID = "{" + id + "}" },
		"resuming at once": func(in *app.Inputs) { in.NewSessionID, in.SessionRef = "2b5e8c7a-3f1d-4b2e-9c6a-7d8e9f0a1b2c", id },
	} {
		t.Run(name+" is a usage error", func(t *testing.T) {
			in := in
			change(&in)

			_, err := app.Setup(context.Background(), in, io.Discard)

			_, isUsage := errors.AsType[*app.UsageError](err)
			assert.True(t, isUsage, "%v", err)
		})
	}
}
