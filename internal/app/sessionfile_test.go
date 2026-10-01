package app_test

import (
	"context"
	"io"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	runnersession "github.com/viktordanov/unreal-agent/harness/session"
	"github.com/viktordanov/unreal-agent/harness/sessionstore/localfile"

	"github.com/viktordanov/uah/internal/app"
	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/internal/sessionfile"
	"github.com/viktordanov/uah/testing/fakellm"
)

// TestSessionFileFollowsTheDocumentedFormat records a session on the
// embedded engine, with the runner at the version in go.mod, and reads it
// by the documented rules (internal/sessionfile), so a change in the
// runner's format fails here before a reader outside uah meets it.
func TestSessionFileFollowsTheDocumentedFormat(t *testing.T) {
	_, in := setupEnv(t)
	t.Setenv("OPENAI_API_KEY", "test-key")
	llm := fakellm.New(t, fakellm.Reply{Commands: []string{"echo hi"}, Reasoning: []string{"look first"}}, fakellm.Reply{Text: "done"})
	in.Provider, in.Model, in.BaseURL = "openai", "gpt-test", llm.URL
	res, err := app.Setup(context.Background(), in, io.Discard)
	require.NoError(t, err)
	s, err := session.Open(context.Background(), res.Engine, res.Options)
	require.NoError(t, err)
	_, err = s.Submit("hello")
	require.NoError(t, err)
	waitFinished(t, s)
	require.NoError(t, s.Close())
	sessions := filepath.Join(in.StateDir, "sessions")

	h, page, err := sessionfile.Read(filepath.Join(sessions, s.ID()+".session.jsonl"), sessionfile.BeforeFirst, 0)
	require.NoError(t, err)

	assert.Equal(t, s.ID(), h.ID)
	var kinds []sessionfile.Kind
	var said, answers, calls []string
	for i, it := range page.Items {
		require.Equal(t, uint64(i+1), it.Sequence)
		kinds = append(kinds, it.Kind)
		switch it.Kind {
		case sessionfile.KindInput:
			var input sessionfile.Input
			require.NoError(t, it.Decode(&input))
			if input.Kind == sessionfile.InputExternal {
				text, err := input.Text()
				require.NoError(t, err)
				said = append(said, text)
			}
		case sessionfile.KindModelResponse:
			var r sessionfile.ModelResponse
			require.NoError(t, it.Decode(&r))
			for _, o := range r.Response.Output {
				if o.Type != sessionfile.OutputMessage {
					continue
				}
				var m sessionfile.Message
				require.NoError(t, o.Decode(&m))
				answers = append(answers, m.Text)
			}
		case sessionfile.KindToolCallStatus:
			var st sessionfile.ToolCallStatus
			require.NoError(t, it.Decode(&st))
			calls = append(calls, st.CallID)
		}
	}
	assert.Equal(t, []string{"hello"}, said)
	assert.Contains(t, answers, "done")
	assert.NotEmpty(t, calls, "the Bash call's status")
	for _, k := range []sessionfile.Kind{sessionfile.KindInput, sessionfile.KindTurn, sessionfile.KindModelResponse, sessionfile.KindToolCallStatus} {
		assert.True(t, slices.Contains(kinds, k), "an item of kind %s", k)
	}

	store, err := localfile.New(sessions)
	require.NoError(t, err)
	runner, err := store.Items(context.Background(), runnersession.ID(s.ID()), 0, 1000)
	require.NoError(t, err)
	require.Len(t, page.Items, len(runner.Items), "the runner's reader sees the same items")
	for i, it := range runner.Items {
		assert.Equal(t, string(it.Kind), string(page.Items[i].Kind))
	}
}
