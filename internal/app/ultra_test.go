package app_test

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/internal/app"
	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/testing/fakellm"
)

// TestEffortUltra: effort ultra, which the runner cannot carry, reaches the
// provider as reasoning effort ultra, and /effort switches it on and off
// between runs.
func TestEffortUltra(t *testing.T) {
	_, in := setupEnv(t)
	t.Setenv("OPENAI_API_KEY", "test-key")
	llm := fakellm.New(t, fakellm.Reply{Text: "one"}, fakellm.Reply{Text: "two"}, fakellm.Reply{Text: "three"})
	in.Provider, in.Model, in.Effort, in.BaseURL = "openai", "gpt-test", "ultra", llm.URL

	res, err := app.Setup(context.Background(), in, io.Discard)
	require.NoError(t, err)
	s, err := session.Open(context.Background(), res.Engine, res.Options)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	run := func(effort string) {
		t.Helper()
		next := res.Options.Settings
		next.Effort = effort
		_, err := s.SetSettings(next)
		require.NoError(t, err)
		_, err = s.Submit("go")
		require.NoError(t, err)
		waitFinished(t, s)
	}

	run("ultra")
	run("high")
	run("ultra")

	var efforts []string
	for _, r := range llm.Requests() {
		efforts = append(efforts, r.Effort)
	}
	assert.Equal(t, []string{"ultra", "high", "ultra"}, efforts)
}
