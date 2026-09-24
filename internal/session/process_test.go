package session_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"
	uaharness "github.com/viktordanov/uagent/harness"
	"github.com/viktordanov/uagent/testing/fixtures"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/engine/process"
	"github.com/viktordanov/uagent-harness/internal/hooks"
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

// TestSession_ProcessEngineSharedBehavior pins what the session does the
// same on both engines, on real runs of the fake runner: the host prompt,
// the session-level hooks (SessionStart, UserPromptSubmit, Stop,
// SessionEnd), the saved settings and when they apply, and one notice per
// configured feature the engine does not run.
func TestSession_ProcessEngineSharedBehavior(t *testing.T) {
	env := harnesstest.NewEnv(t)
	t.Setenv("FAKERUNNER_FIXTURE", fixtures.Path("simple.jsonl"))
	t.Setenv("FAKERUNNER_CAPTURE", env.Capture)
	eng := process.New(uaharness.Config{RunnerPath: harnesstest.FakeRunner(t), StateDir: env.StateDir, KillGrace: time.Second, Getenv: env.Getenv})
	marks := t.TempDir()
	runner, err := hooks.New([]hooks.Hook{
		{Event: hooks.SessionStart, Command: "echo 'from SessionStart'", Source: hooks.SourceUser},
		{Event: hooks.UserPromptSubmit, Command: "echo 'from UserPromptSubmit'", Source: hooks.SourceUser},
		{Event: hooks.Stop, Command: "touch " + filepath.Join(marks, "stop"), Source: hooks.SourceUser},
		{Event: hooks.SessionEnd, Command: "touch " + filepath.Join(marks, "end"), Source: hooks.SourceUser},
	}, nil, env.Workspace)
	require.NoError(t, err)
	settings := session.Settings{
		Provider: "openai-codex", Model: "gpt-6-sol", Effort: "high", Workspace: env.Workspace, SystemPrompt: "the host prompt",
	}.WithMode(approval.ModeReadOnly)
	sessionsDir := filepath.Join(env.StateDir, "sessions")
	s, err := session.Open(context.Background(), eng, session.Options{
		Settings: settings, Hooks: runner, SessionsDir: sessionsDir, Source: session.SourceRun,
		Uses: []engine.Feature{engine.FeatureMCP, engine.FeaturePreToolUseHooks, engine.FeatureCompaction},
	})
	require.NoError(t, err)
	h := &harness{t: t, s: s}

	_, err = s.Submit("hi")
	require.NoError(t, err)
	h.until(isType[core.RunFinished])
	h.until(isType[session.Idle])
	var notices []string
	for _, e := range h.events {
		if n, ok := e.(session.Notice); ok {
			notices = append(notices, n.Message)
		}
	}
	assert.Equal(t, []string{
		"compaction: not supported by the process engine (/compact and /clear are not available and the context is never compacted); use the embedded engine",
		"PreToolUse hooks: not supported by the process engine (they do not run); use the embedded engine",
		"MCP servers: not supported by the process engine (they do not start); use the embedded engine",
	}, notices, "one notice per configured feature the engine lacks, in the table's order")

	var req struct {
		SystemPrompt string `json:"system_prompt"`
		Messages     []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	stdin, err := os.ReadFile(filepath.Join(env.Capture, "stdin.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(stdin, &req))
	assert.Equal(t, "the host prompt", req.SystemPrompt)
	require.Len(t, req.Messages, 1)
	assert.Equal(t, "hi\n\nfrom SessionStart\n\nfrom UserPromptSubmit", req.Messages[0].Content, "the hooks' context goes with the message")
	assert.FileExists(t, filepath.Join(marks, "stop"))

	next := settings
	next.Effort = "low"
	applied, err := s.SetSettings(next)
	require.NoError(t, err)
	assert.Equal(t, session.AppliedNextRun, applied)
	sc, found, err := session.ReadSidecar(sessionsDir, s.ID())
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, sc.Settings)
	assert.Equal(t, session.Saved{Provider: "openai-codex", Model: "gpt-6-sol", Effort: "low", Mode: approval.ModeReadOnly}, *sc.Settings)
	require.ErrorIs(t, s.Compact(), session.ErrNoCompaction)
	require.ErrorIs(t, s.Clear(), session.ErrNoCompaction)
	_, ok := s.ContextUsage()
	assert.False(t, ok, "no /context")
	_, ok = s.MCPServers()
	assert.False(t, ok, "no MCP servers")

	require.NoError(t, s.Close())
	assert.FileExists(t, filepath.Join(marks, "end"))
}
