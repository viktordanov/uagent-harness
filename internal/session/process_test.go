package session_test

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
	"github.com/viktordanov/uagent/testing/fixtures"

	"github.com/viktordanov/uagent-harness/internal/engine/process"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/testing/harnesstest"
)

// TestSession_ProcessEngine drives real runs of the fake runner: a first
// message, a steer that interrupts and restarts, then history and transcript.
func TestSession_ProcessEngine(t *testing.T) {
	env := harnesstest.NewEnv(t)
	t.Setenv("FAKERUNNER_FIXTURE", fixtures.Path("timeout.jsonl"))
	t.Setenv("FAKERUNNER_ECHO", "1")
	t.Setenv("FAKERUNNER_HANG", "1")
	t.Setenv("FAKERUNNER_CAPTURE", env.Capture)
	eng := process.New(uaharness.Config{
		RunnerPath: harnesstest.FakeRunner(t), StateDir: env.StateDir,
		KillGrace: 300 * time.Millisecond, Getenv: env.Getenv,
	})
	settings := session.Settings{Provider: "openai-codex", Model: "gpt-6-sol", Effort: "high", Workspace: env.Workspace}
	s, err := session.Open(context.Background(), eng, session.Options{Settings: settings})
	require.NoError(t, err)
	h := &harness{t: t, s: s}
	t.Cleanup(func() { _ = s.Close() })

	first, err := s.Submit("fix the test")
	require.NoError(t, err)
	delivered := h.until(isType[session.InputDelivered]).(session.InputDelivered)
	assert.Equal(t, first.ID, delivered.ID)
	waitForFile(t, filepath.Join(env.Capture, "hung.pids"))

	steer, err := s.SteerNow("use the fixture instead")
	require.NoError(t, err)
	interrupted := h.until(isType[core.RunFinished]).(core.RunFinished)
	assert.Equal(t, core.StatusInterrupted, interrupted.Result.Status)
	delivered = h.until(isType[session.InputDelivered]).(session.InputDelivered)
	assert.Equal(t, steer.ID, delivered.ID, "the steer starts a new run with the message")

	require.NoError(t, s.Interrupt())
	h.until(isType[session.Idle])

	infos, err := session.Sessions(env.StateDir)
	require.NoError(t, err)
	require.Len(t, infos, 1)
	info := infos[0]
	assert.Equal(t, s.ID(), info.ID)
	assert.Equal(t, 2, info.Runs)
	assert.Equal(t, "fix the test", info.FirstPrompt)
	assert.Equal(t, core.StatusInterrupted, info.Status)
	assert.Equal(t, env.Workspace, info.Workspace)

	runs, err := session.Load(env.StateDir, s.ID())
	require.NoError(t, err)
	require.Len(t, runs, 2)
	assert.Equal(t, []string{"fix the test"}, userTexts(runs[0].Events)[:1])
	assert.Equal(t, []string{"use the fixture instead"}, userTexts(runs[1].Events)[:1])
	assert.True(t, runs[0].Record.Result.StartedAt.Before(runs[1].Record.Result.StartedAt))
}

func userTexts(events []core.Event) []string {
	var out []string
	for _, e := range events {
		if m, ok := e.(core.UserMessage); ok {
			out = append(out, m.Text)
		}
	}

	return out
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	require.Eventually(t, func() bool {
		_, err := os.Stat(path)

		return err == nil
	}, 5*time.Second, 10*time.Millisecond, "waiting for %s", path)
	require.NoError(t, os.Remove(path))
}
