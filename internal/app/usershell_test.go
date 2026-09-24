package app_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/app"
	"github.com/viktordanov/uagent-harness/internal/sandbox"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/usershell"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

// TestSetup_UserShellCommand runs a command the user typed in a session
// on the embedded engine: no model request goes out for it, the next
// request carries its record in Codex's format before the message, and
// the saved run keeps it, so a resumed transcript shows it.
func TestSetup_UserShellCommand(t *testing.T) {
	e, in := setupEnv(t)
	t.Setenv("OPENAI_API_KEY", "test-key")
	llm := fakellm.New(t, fakellm.Reply{Text: "it printed hi and failed"})
	in.Provider, in.Model, in.BaseURL = "openai", "gpt-test", llm.URL
	res, err := app.Setup(context.Background(), in, io.Discard)
	require.NoError(t, err)
	s, err := session.Open(context.Background(), res.Engine, res.Options)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	ran, err := s.RunShell(t.Context(), "echo hi; ls missing-file; exit 1")
	require.NoError(t, err)
	assert.Equal(t, 1, ran.ExitCode)
	assert.Equal(t, sandbox.FullAccess, ran.Sandbox, "the user's own command, as in Codex")
	assert.Empty(t, llm.Requests(), "running a command does not start a turn")

	_, err = s.Submit("why did it fail?")
	require.NoError(t, err)
	waitFinished(t, s)

	reqs := llm.Requests()
	require.Len(t, reqs, 1)
	texts := reqs[0].UserTexts
	require.GreaterOrEqual(t, len(texts), 2)
	record := texts[len(texts)-2]
	assert.Equal(t, ran.Text(), record)
	assert.Contains(t, record, "<user_shell_command>\n<command>\necho hi; ls missing-file; exit 1\n</command>\n<result>\nExit code: 1\nDuration: ")
	assert.Contains(t, record, " seconds\nOutput:\nhi\n")
	assert.Contains(t, record, "missing-file", "stderr is in the output")
	assert.Equal(t, "why did it fail?", texts[len(texts)-1])

	runs, err := session.Load(e.StateDir, s.ID())
	require.NoError(t, err)
	require.Len(t, runs, 1)
	var saved []usershell.Record
	for _, ev := range runs[0].Events {
		if m, ok := ev.(core.UserMessage); ok {
			if r, ok := usershell.Parse(m.Text); ok {
				saved = append(saved, r)
			}
		}
	}
	require.Len(t, saved, 1, "the saved run has the record")
	assert.Equal(t, ran.Command, saved[0].Command)
}

// TestSetup_UserShellSandbox runs the user's commands like the agent's
// with user_shell_sandbox: in the permission mode's sandbox, and refused
// by a forbid rule.
func TestSetup_UserShellSandbox(t *testing.T) {
	_, in := setupEnv(t)
	t.Setenv("OPENAI_API_KEY", "test-key")
	in.Provider, in.Model, in.BaseURL = "openai", "gpt-test", fakellm.New(t).URL
	require.NoError(t, os.MkdirAll(filepath.Dir(in.ConfigPath), 0o700))
	require.NoError(t, os.WriteFile(in.ConfigPath, []byte("user_shell_sandbox = true\npermission_mode = \"read-only\"\n\n[approvals]\nforbid = [\"rm\"]\n"), 0o600))
	res, err := app.Setup(context.Background(), in, io.Discard)
	require.NoError(t, err)
	s, err := session.Open(context.Background(), res.Engine, res.Options)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	refused, err := s.RunShell(t.Context(), "rm -rf build")
	require.NoError(t, err)
	assert.Equal(t, "not run: a rule forbids this command.", refused.Refused)

	ran, err := s.RunShell(t.Context(), "true")
	require.NoError(t, err)
	if ran.Sandbox != sandbox.FullAccess { // no sandbox on this system: it runs without one
		assert.Equal(t, sandbox.ReadOnly, ran.Sandbox, "the session's permission mode picks the sandbox")
	}
}
