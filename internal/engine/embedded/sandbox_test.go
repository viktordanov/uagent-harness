package embedded_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine/embedded"
	"github.com/viktordanov/uagent-harness/internal/sandbox"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

// TestEmbedded_Sandbox runs real commands in the workspace-write sandbox:
// writes inside the workspace work, writes elsewhere and to .git fail with a
// hint, and escalations are refused with a reason.
func TestEmbedded_Sandbox(t *testing.T) {
	ws := t.TempDir()
	policy := sandbox.Policy{Mode: sandbox.WorkspaceWrite, Workspace: ws}
	if _, err := policy.Wrap([]string{"/bin/sh"}); err != nil {
		t.Skipf("no sandbox here: %v", err)
	}
	outside, err := os.MkdirTemp(userCache(t), "uah-sandbox-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(outside) })
	require.NoError(t, os.MkdirAll(filepath.Join(ws, ".git"), 0o700))

	e := newEnv(t,
		fakellm.Reply{
			Commands:  []string{"echo ok > inside.txt", "echo no > " + filepath.Join(outside, "x.txt"), "echo no > .git/config"},
			Escalated: []string{"curl -s https://example.com"},
		},
		fakellm.Reply{Text: "done"},
	)
	e.Workspace = ws
	eng := embedded.New(embedded.Config{
		StateDir: e.StateDir, Provider: "openai", Getenv: e.getenv,
		Sandbox: &policy, SandboxDir: filepath.Join(e.StateDir, "sandbox"),
	})
	s, err := session.Open(context.Background(), eng, session.Options{Settings: e.settings()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	ev := &events{t: t, s: s}

	_, err = s.Submit("try some writes")
	require.NoError(t, err)
	result := ev.finished()
	assert.Equal(t, core.StatusOK, result.Status)

	assert.FileExists(t, filepath.Join(ws, "inside.txt"))
	assert.NoFileExists(t, filepath.Join(outside, "x.txt"))
	assert.NoFileExists(t, filepath.Join(ws, ".git", "config"))
	reqs := e.llm.Requests()
	assert.Contains(t, reqs[0].Tools["Bash"], "sandbox_permissions", "the model is offered escalation")
	outputs := strings.Join(reqs[len(reqs)-1].ToolOutputs, "\n---\n")
	assert.Equal(t, 2, strings.Count(outputs, "sandbox likely blocked this"), outputs)
	assert.Contains(t, outputs, "not run: running outside the workspace-write sandbox needs the user's approval")
}

func userCache(t *testing.T) string {
	t.Helper()
	dir, err := os.UserCacheDir()
	require.NoError(t, err)

	return dir
}
