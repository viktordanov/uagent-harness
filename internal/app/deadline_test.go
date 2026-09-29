package app_test

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/app"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

// TestSetup_NoWallClockLimit pins that a turn runs as long as it needs:
// set up as the TUI is, a run reaches uagent's harness on the embedded
// engine with no timeout, so the harness puts no deadline on it.
func TestSetup_NoWallClockLimit(t *testing.T) {
	_, in := setupEnv(t)
	t.Setenv("OPENAI_API_KEY", "test-key")
	llm := fakellm.New(t, fakellm.Reply{Text: "done"})
	in.Provider, in.Model, in.BaseURL = "openai", "gpt-test", llm.URL

	res, err := app.Setup(context.Background(), in, io.Discard)
	require.NoError(t, err)
	opts := res.Options
	opts.Source = session.SourceTUI
	s, err := session.Open(context.Background(), res.Engine, opts)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	_, err = s.Submit("hello")
	require.NoError(t, err)

	var finished *core.RunFinished
	deadline := time.After(30 * time.Second)
	for finished == nil {
		select {
		case e := <-s.Events():
			if f, ok := e.(core.RunFinished); ok {
				finished = &f
			}
		case <-deadline:
			t.Fatal("the run did not finish")
		}
	}

	assert.Equal(t, core.StatusOK, finished.Result.Status)
	assert.Zero(t, finished.Result.Request.Timeout, "the request uagent's harness ran")
}
