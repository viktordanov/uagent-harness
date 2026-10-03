package embedded_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/internal/engine/embedded"
	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/testing/fakellm"
)

// TestEmbedded_SendsVerbosity: as in Codex, a model whose catalog entry
// supports verbosity gets model_verbosity, else its default_verbosity, and
// a model no entry describes gets none, also after /model switches to it.
func TestEmbedded_SendsVerbosity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, verbosity, want string
	}{
		{name: "the model's default", want: "low"},
		{name: "model_verbosity", verbosity: "high", want: "high"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gate := make(chan struct{})
			e := newEnv(t, fakellm.Reply{Commands: []string{"true"}, Gate: gate}, fakellm.Reply{Text: "done"})
			eng := embedded.New(embedded.Config{StateDir: e.StateDir, Provider: "openai", Getenv: e.getenv, Verbosity: tt.verbosity})
			settings := e.settings()
			settings.Model = "gpt-5.5" // the bundled entry: support_verbosity, default low
			s, err := session.Open(context.Background(), eng, session.Options{Settings: settings})
			require.NoError(t, err)
			t.Cleanup(func() { _ = s.Close() })
			ev := &events{t: t, s: s}

			_, err = s.Submit("go")
			require.NoError(t, err)
			waitSeen(t, e.llm, 1)
			settings.Model = "gpt-test"
			_, err = s.SetSettings(settings)
			require.NoError(t, err)
			close(gate)
			ev.finished()

			reqs := e.llm.Requests()
			require.Len(t, reqs, 2)
			assert.Equal(t, "gpt-5.5", reqs[0].Model)
			assert.Equal(t, tt.want, reqs[0].Verbosity)
			assert.Equal(t, "gpt-test", reqs[1].Model)
			assert.Empty(t, reqs[1].Verbosity, "a model no entry describes gets no text field")
		})
	}
}
