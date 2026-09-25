package app_test

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/app"
	"github.com/viktordanov/uagent-harness/internal/contextusage"
	"github.com/viktordanov/uagent-harness/internal/instructions"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

// TestSetup_ModelInstructionsFile sets model_instructions_file to a path
// relative to the user file, as Codex resolves it: a missing or empty file
// stops the session, and the file's text replaces the runner's host prompt
// in the model request, after the runner's preamble and before AGENTS.md. A subagent's request
// carries the same system prompt, and /context counts the file as the
// system prompt.
func TestSetup_ModelInstructionsFile(t *testing.T) {
	e, in := setupEnv(t)
	t.Setenv("OPENAI_API_KEY", "test-key")
	writeConfig(t, &in, "model_instructions_file = \"prompts/system.md\"\n")
	prompt := filepath.Join(filepath.Dir(in.ConfigPath), "prompts", "system.md")

	_, err := app.Setup(context.Background(), in, io.Discard)
	require.ErrorContains(t, err, "failed to read model_instructions_file", "a missing file stops the session, as in Codex")
	require.ErrorContains(t, err, prompt, "relative to the user file")
	var usage *app.UsageError
	assert.True(t, errors.As(err, &usage), "a usage error")
	writeFile(t, prompt, " \n")
	_, err = app.Setup(context.Background(), in, io.Discard)
	require.ErrorContains(t, err, "is empty")

	writeFile(t, prompt, "# Project instructions\n\nBASE-PROMPT: answer briefly.\n")
	writeFile(t, filepath.Join(e.Workspace, "AGENTS.md"), "Use tabs in Go files.")
	llm := fakellm.New(t,
		fakellm.Reply{Calls: []fakellm.Call{{Name: "spawn_agent", Args: `{"message":"CHILD-S check the build"}`}}},
		fakellm.Reply{From: func(req fakellm.Request) fakellm.Reply {
			id := strings.Split(strings.Split(req.ToolOutputs[0], `"agent_id":"`)[1], `"`)[0]
			return fakellm.Reply{Calls: []fakellm.Call{{Name: "wait_agent", Args: `{"targets":["` + id + `"]}`}}}
		}},
		fakellm.Reply{Text: "done", InputTokens: 5_000},
	)
	llm.Route("CHILD-S", fakellm.Reply{Text: "the build is fine"})
	in.Provider, in.Model, in.BaseURL = "openai", "gpt-test", llm.URL
	res, err := app.Setup(context.Background(), in, io.Discard)
	require.NoError(t, err)
	want := "# Project instructions\n\nBASE-PROMPT: answer briefly.\n\n" + instructions.ProjectHeader
	assert.True(t, strings.HasPrefix(res.Options.Settings.SystemPrompt, want), res.Options.Settings.SystemPrompt)
	s, err := session.Open(context.Background(), res.Engine, res.Options)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	_, err = s.Submit("delegate")
	require.NoError(t, err)
	waitFinished(t, s)

	reqs := llm.Requests()
	root := reqs[0]
	assert.Contains(t, root.System, "\n\n"+want, "the file follows the runner's preamble, and the instructions follow it")
	assert.Contains(t, root.System, "Use tabs in Go files.")
	assert.NotContains(t, root.System, "isolated sandbox container", "the runner's host prompt is replaced")
	child := slices.IndexFunc(reqs, func(r fakellm.Request) bool {
		return slices.ContainsFunc(r.UserTexts, func(u string) bool { return strings.HasPrefix(u, "CHILD-S") })
	})
	require.GreaterOrEqual(t, child, 0)
	assert.Equal(t, root.System, reqs[child].System, "a subagent gets the same system prompt")

	u, ok := s.ContextUsage()
	require.True(t, ok)
	cats := map[string]contextusage.Category{}
	for _, c := range u.Categories {
		cats[c.Name] = c
	}
	assert.Positive(t, cats[contextusage.SystemPrompt].Tokens)
	require.Len(t, cats[contextusage.Instructions].Items, 1, "the file's own heading does not start the instructions")
	assert.Equal(t, filepath.Join(e.Workspace, "AGENTS.md"), cats[contextusage.Instructions].Items[0].Name)

	t.Run("without instructions", func(t *testing.T) {
		in := in
		in.NoInstructions = true

		res, err := app.Setup(context.Background(), in, io.Discard)

		require.NoError(t, err)
		assert.Equal(t, "# Project instructions\n\nBASE-PROMPT: answer briefly.\n", res.Options.Settings.SystemPrompt)
	})
}

// TestSetup_ModelInstructionsFileFromTheProject: a trusted project's
// model_instructions_file wins over the user's, relative to the project
// file.
func TestSetup_ModelInstructionsFileFromTheProject(t *testing.T) {
	e, in := setupEnv(t)
	writeConfig(t, &in, "model_instructions_file = \"user.md\"\n\n[projects.\""+e.Workspace+"\"]\ntrusted = true\n")
	writeFile(t, filepath.Join(filepath.Dir(in.ConfigPath), "user.md"), "USER-PROMPT")
	writeFile(t, filepath.Join(e.Workspace, ".uah", "config.toml"), "model_instructions_file = \"prompts/mine.md\"\n")
	writeFile(t, filepath.Join(e.Workspace, ".uah", "prompts", "mine.md"), "PROJECT-PROMPT")
	in.NoInstructions = true

	res, err := app.Setup(context.Background(), in, io.Discard)

	require.NoError(t, err)
	assert.Equal(t, "PROJECT-PROMPT\n", res.Options.Settings.SystemPrompt)
}
