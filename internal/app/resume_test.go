package app_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/app"
	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

// TestSetup_ResumeRestoresSessionSettings runs a session, changes its
// model, effort, fast mode, and permission mode after the run, and resumes
// it: the sidecar's settings beat the configuration, a flag beats them,
// and a sidecar without settings falls back to the last run's request.
func TestSetup_ResumeRestoresSessionSettings(t *testing.T) {
	_, in := setupEnv(t)
	t.Setenv("OPENAI_API_KEY", "test-key")
	llm := fakellm.New(t, fakellm.Reply{Text: "hi"})
	require.NoError(t, os.MkdirAll(filepath.Dir(in.ConfigPath), 0o700))
	require.NoError(t, os.WriteFile(in.ConfigPath, []byte("model = \"cfg-model\"\neffort = \"high\"\npermission_mode = \"auto\"\n"), 0o600))
	in.Provider, in.Model, in.Effort, in.BaseURL = "openai", "gpt-test", "medium", llm.URL

	res, err := app.Setup(context.Background(), in, io.Discard)
	require.NoError(t, err)
	opts := res.Options
	opts.Source = session.SourceTUI
	s, err := session.Open(context.Background(), res.Engine, opts)
	require.NoError(t, err)
	_, err = s.Submit("hello")
	require.NoError(t, err)
	waitFinished(t, s)
	next := opts.Settings.WithMode(approval.ModeReadOnly)
	next.Model, next.Effort, next.ServiceTier = "gpt-other", "low", "priority"
	_, err = s.SetSettings(next)
	require.NoError(t, err)
	require.NoError(t, s.Close())

	resume := in
	resume.SessionRef, resume.Provider, resume.Model, resume.Effort = s.ID(), "", "", ""

	t.Run("the session's settings beat the configuration", func(t *testing.T) {
		res, err := app.Setup(context.Background(), resume, io.Discard)
		require.NoError(t, err)

		got := res.Options.Settings
		assert.Equal(t, [4]string{"openai", "gpt-other", "low", "priority"}, [4]string{got.Provider, got.Model, got.Effort, got.ServiceTier})
		assert.Equal(t, approval.ModeReadOnly, got.Mode)
		assert.Equal(t, "read-only", got.Sandbox)

		rep, err := app.Inspect(context.Background(), resume)
		require.NoError(t, err)
		for _, st := range rep.Settings {
			switch st.Key {
			case "model", "effort", "fast", "permission_mode", "sandbox_mode":
				assert.Equal(t, "session", st.SourceText(), st.Key)
			}
		}
	})

	t.Run("a flag beats the session", func(t *testing.T) {
		in := resume
		in.Effort, in.Sandbox, in.Fast, in.FastSet = "max", "workspace-write", false, true

		res, err := app.Setup(context.Background(), in, io.Discard)
		require.NoError(t, err)

		got := res.Options.Settings
		assert.Equal(t, [3]string{"gpt-other", "max", ""}, [3]string{got.Model, got.Effort, got.ServiceTier})
		assert.Equal(t, approval.ModeWorkspace, got.Mode)
	})

	t.Run("an older sidecar without settings falls back to the last run", func(t *testing.T) {
		sessions := filepath.Join(in.StateDir, "sessions")
		require.NoError(t, os.WriteFile(filepath.Join(sessions, s.ID()+".uah.json"), []byte(`{"source":"tui","created":"2026-01-01T00:00:00Z"}`+"\n"), 0o600))

		res, err := app.Setup(context.Background(), resume, io.Discard)
		require.NoError(t, err)

		got := res.Options.Settings
		assert.Equal(t, [3]string{"gpt-test", "medium", ""}, [3]string{got.Model, got.Effort, got.ServiceTier}, "the run's request")
		assert.Equal(t, approval.ModeAuto, got.Mode, "the configured mode")
	})
}

// TestPrecedenceDocNamesTheSavedSettings pins that the configuration
// reference's precedence list names every setting a session keeps in its
// sidecar, by its key.
func TestPrecedenceDocNamesTheSavedSettings(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "configuration.md"))
	require.NoError(t, err)
	var line string
	for l := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(l, "3. The resumed session") {
			line = l
		}
	}
	require.NotEmpty(t, line, "the precedence list has the resumed session third")
	for f := range reflect.TypeFor[session.Saved]().Fields() {
		key, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		assert.Contains(t, line, "`"+key+"`", "the doc names %s", f.Name)
	}
}
