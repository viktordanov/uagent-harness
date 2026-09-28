package main_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"
	uaharness "github.com/viktordanov/uagent/harness"

	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
	"github.com/viktordanov/uagent-harness/testing/harnesstest"
)

// TestRunResumesAProcessSession: a session the real runner started, as
// the process engine of earlier versions left them, resumes with
// `uah run --session` on the embedded engine, which replays its history.
func TestRunResumesAProcessSession(t *testing.T) {
	if testing.Short() {
		t.Skip("builds unreal-agent-runner")
	}
	e := harnesstest.NewEnv(t)
	llm := fakellm.New(t, fakellm.Reply{Text: "noted"}, fakellm.Reply{Text: "7"})
	t.Setenv("OPENAI_API_KEY", "test-key") // the runner reads its environment
	runner := harnesstest.RunnerEngine(uaharness.Config{RunnerPath: harnesstest.RealRunner(t), StateDir: e.StateDir, Getenv: os.Getenv, KillGrace: time.Second})
	settings := session.Settings{Provider: "openai", Model: "gpt-test", Effort: "high", Workspace: e.Workspace, BaseURL: llm.URL}
	s, err := session.Open(context.Background(), runner, session.Options{
		Settings: settings, SessionsDir: filepath.Join(e.StateDir, "sessions"), Source: session.SourceTUI,
	})
	require.NoError(t, err)
	_, err = s.Submit("remember the number 7")
	require.NoError(t, err)
	var finished bool
	for ev := range s.Events() {
		if f, ok := ev.(core.RunFinished); ok {
			require.Equal(t, core.StatusOK, f.Result.Status, "%+v", f.Result)
			finished = true
		}
		if _, ok := ev.(session.Idle); ok && finished {
			break
		}
		if n, ok := ev.(session.Notice); ok && n.Level == session.LevelError {
			require.Fail(t, n.Message)
		}
	}
	require.NoError(t, s.Close())
	env := []string{
		"UAH_STATE_DIR=" + e.StateDir, "UNREAL_HARNESS_LLM_PROVIDER=", "UNREAL_HARNESS_LLM_MODEL=",
		"UAH_HOME=" + filepath.Join(e.StateDir, "..", "home"),
	}

	res := uahWith(t, env, "", "run", "--session", s.ID()[:8], "--base-url", llm.URL, "what was the number?")

	require.Equal(t, 0, res.code, res.stderr)
	assert.Equal(t, "7\n", res.stdout)
	assert.Contains(t, res.stderr, "(resumed)")
	reqs := llm.Requests()
	require.Len(t, reqs, 2)
	assert.Equal(t, []string{"remember the number 7", "what was the number?"}, reqs[1].UserTexts, "the embedded run replays the runner's history")
}
