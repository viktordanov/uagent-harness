package main_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/testing/fakellm"
	"github.com/viktordanov/uagent-harness/testing/harnesstest"
)

// writeUserConfig writes the user's config.toml for a test's environment.
func writeUserConfig(t *testing.T, e *harnesstest.Env, text string) {
	t.Helper()
	dir := filepath.Join(e.StateDir, "..", "config", "uagent")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.toml"), []byte(text), 0o600))
}

// TestRunProcess_Notices: on the process engine, each configured feature it
// does not run gets one notice when the session opens.
func TestRunProcess_Notices(t *testing.T) {
	e, env := fakeEnv(t, "simple.jsonl")
	writeUserConfig(t, e, "[mcp_servers.docs]\ncommand = \"true\"\n\n[[hooks.PreToolUse]]\ncommand = \"true\"\n")

	res := uahWith(t, env, "", "run", "-C", e.Workspace, "hi")

	require.Equal(t, 0, res.code, res.stderr)
	for _, feature := range []string{"PreToolUse hooks", "MCP servers"} {
		assert.Equal(t, 1, strings.Count(res.stderr, feature+": not supported by the process engine"), res.stderr)
	}
}

// TestRunProcess_ForbidRule runs the real runner on the process engine: uah
// is the runner's shell gate, so a forbidden command is refused before it
// runs and the model hears the rule's reason.
func TestRunProcess_ForbidRule(t *testing.T) {
	if testing.Short() {
		t.Skip("builds unreal-agent-runner")
	}
	e := harnesstest.NewEnv(t)
	llm := fakellm.New(t, fakellm.Reply{Commands: []string{"rm -f keep", "echo ok > made"}}, fakellm.Reply{Text: "done"})
	require.NoError(t, os.WriteFile(filepath.Join(e.Workspace, "keep"), []byte("x"), 0o600))
	writeUserConfig(t, e, "[approvals]\nforbid = [\"rm\"]\n")
	env := []string{
		"UAH_ENGINE=process", "UAGENT_RUNNER=" + harnesstest.RealRunner(t),
		"UAGENT_STATE_DIR=" + e.StateDir, "OPENAI_API_KEY=test-key",
		"UNREAL_HARNESS_LLM_PROVIDER=", "UNREAL_HARNESS_LLM_MODEL=",
		"XDG_CONFIG_HOME=" + filepath.Join(e.StateDir, "..", "config"),
	}

	res := uahWith(t, env, "", "run", "--provider", "openai", "-m", "gpt-test", "--base-url", llm.URL, "-C", e.Workspace, "tidy up")

	require.Equal(t, 0, res.code, res.stderr)
	assert.Equal(t, "done\n", res.stdout)
	assert.FileExists(t, filepath.Join(e.Workspace, "keep"), "the forbidden command did not run")
	assert.FileExists(t, filepath.Join(e.Workspace, "made"), "the other one ran")
	reqs := llm.Requests()
	require.Len(t, reqs, 2)
	require.Len(t, reqs[1].ToolOutputs, 2)
	// The two commands run at once, so their results come back in either order.
	assert.Contains(t, strings.Join(reqs[1].ToolOutputs, "\n"), "not run: a rule forbids this command")
}
