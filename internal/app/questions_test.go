package app_test

import (
	"context"
	"io"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/internal/app"
	"github.com/viktordanov/uah/internal/engine"
	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/testing/fakellm"
)

// TestSetup_QuestionTool: a TUI session offers request_user_input and its
// prompt names it; [tools.experimental_request_user_input] enabled = false,
// in the user file or a config.d layer, or UAH_REQUEST_USER_INPUT=off,
// takes the tool away and gives the prompt item 63's text again, and the
// variable wins over the files.
func TestSetup_QuestionTool(t *testing.T) {
	off := "[tools.experimental_request_user_input]\nenabled = false\n"
	for _, tc := range []struct {
		name, user, layer, env string
		want                   bool
	}{
		{name: "default", want: true},
		{name: "user file", user: off},
		{name: "config.d layer", layer: off},
		{name: "variable", env: "off"},
		{name: "variable over the file", user: off, env: "on", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, in := setupEnv(t)
			t.Setenv("OPENAI_API_KEY", "test-key")
			t.Setenv(app.EnvRequestUserInput, tc.env)
			writeConfig(t, &in, tc.user)
			if tc.layer != "" {
				writeFile(t, filepath.Join(filepath.Dir(in.ConfigPath), "config.d", "web-tty.toml"), tc.layer)
			}
			llm := fakellm.New(t, fakellm.Reply{Text: "done"})
			in.Provider, in.Model, in.BaseURL, in.Interactive = "openai", "gpt-test", llm.URL, true
			in.RequestUserInput = tc.env
			res, err := app.Setup(context.Background(), in, io.Discard)
			require.NoError(t, err)
			s, err := session.Open(context.Background(), res.Engine, res.Options)
			require.NoError(t, err)
			t.Cleanup(func() { _ = s.Close() })
			_, err = s.Submit("hi")
			require.NoError(t, err)
			waitFinished(t, s)

			req := llm.Requests()[0]
			if tc.want {
				assert.Contains(t, req.ToolNames, engine.QuestionToolName)
				assert.Contains(t, req.System, "When the `request_user_input` tool is available")
			} else {
				assert.NotContains(t, req.ToolNames, engine.QuestionToolName)
				assert.NotContains(t, req.System, "request_user_input")
				assert.Contains(t, req.System, "You can ask multiple questions in a single final message. ")
			}
		})
	}

	_, in := setupEnv(t)
	in.RequestUserInput = "maybe"
	_, err := app.Setup(context.Background(), in, io.Discard)
	assert.ErrorContains(t, err, "invalid UAH_REQUEST_USER_INPUT \"maybe\" (want on or off)")
}
