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

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/engine/process"
	"github.com/viktordanov/uagent-harness/internal/sandbox"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/usershell"
	"github.com/viktordanov/uagent-harness/testing/harnesstest"
)

func shellRunner(t *testing.T) *usershell.Runner {
	t.Helper()

	return &usershell.Runner{Dir: t.TempDir(), Policy: sandbox.Policy{Workspace: t.TempDir()}, Shell: "/bin/sh"}
}

func newShellHarness(t *testing.T, caps engine.Capabilities) *harness {
	t.Helper()
	eng := newFakeEngine(caps)
	s, err := session.Open(context.Background(), eng, session.Options{Settings: settings(), Shell: shellRunner(t)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	return &harness{t: t, eng: eng, s: s}
}

// TestSession_RunShell runs a command the user typed while idle: it never
// starts a run, and its record goes first with the next message, with the
// command's ID, as Session.Inject does.
func TestSession_RunShell(t *testing.T) {
	h := newShellHarness(t, engine.Capabilities{})

	res, err := h.s.RunShell(t.Context(), "echo hi; exit 2")
	require.NoError(t, err)
	assert.Equal(t, 2, res.ExitCode)
	started := h.until(isType[session.ShellStarted]).(session.ShellStarted)
	assert.Equal(t, "echo hi; exit 2", started.Command)
	out := h.until(isType[session.ShellOutput]).(session.ShellOutput)
	assert.Equal(t, "hi\n", out.Text)
	finished := h.until(isType[session.ShellFinished]).(session.ShellFinished)
	assert.Equal(t, started.ID, finished.ID)
	select {
	case <-h.eng.started:
		t.Fatal("a command the user ran started a run")
	case <-time.After(100 * time.Millisecond):
	}

	_, err = h.s.Submit("why did it fail?")
	require.NoError(t, err)
	run := h.nextRun()
	require.Len(t, run.req.Messages, 2)
	assert.Equal(t, started.ID, run.req.Messages[0].ID)
	assert.Equal(t, res.Text(), run.req.Messages[0].Text)
	assert.Contains(t, run.req.Messages[0].Text, "<command>\necho hi; exit 2\n</command>\n<result>\nExit code: 2\n")
	assert.Equal(t, "why did it fail?", run.req.Messages[1].Text)
}

// TestSession_RunShellWhileRunning runs the command at once while the
// agent works, as Codex does, without sending it into the live run; it
// goes with the next message.
func TestSession_RunShellWhileRunning(t *testing.T) {
	h := newShellHarness(t, engine.Capabilities{LiveInput: true})
	_, err := h.s.Submit("work")
	require.NoError(t, err)
	run := h.nextRun()

	res, err := h.s.RunShell(t.Context(), "echo during")
	require.NoError(t, err)
	h.until(isType[session.ShellFinished])
	assert.Empty(t, run.sent, "not sent into the live run")
	run.finish(core.StatusOK)
	h.until(isType[session.Idle])

	_, err = h.s.Submit("next")
	require.NoError(t, err)
	next := h.nextRun()
	require.Len(t, next.req.Messages, 2)
	assert.Equal(t, res.Text(), next.req.Messages[0].Text)
}

// TestSession_InterruptStopsShell stops a running command with the
// session's interrupt (esc esc), and the agent still hears of it.
func TestSession_InterruptStopsShell(t *testing.T) {
	h := newShellHarness(t, engine.Capabilities{})
	done := make(chan usershell.Result, 1)
	go func() {
		res, err := h.s.RunShell(t.Context(), "sleep 30")
		assert.NoError(t, err)
		done <- res
	}()
	h.until(isType[session.ShellStarted])

	require.NoError(t, h.s.Interrupt())
	select {
	case res := <-done:
		assert.True(t, res.Canceled)
		assert.Equal(t, usershell.ExitNotRun, res.ExitCode)
	case <-time.After(10 * time.Second):
		t.Fatal("the interrupt did not stop the command")
	}
}

func TestSession_RunShellWithoutRunner(t *testing.T) {
	h := newHarness(t, engine.Capabilities{})

	_, err := h.s.RunShell(t.Context(), "true")
	require.ErrorIs(t, err, session.ErrNoShell)
}

// TestSession_RunShellProcessEngine runs a command in uah itself on the
// process engine, and the record reaches the runner with the next message.
func TestSession_RunShellProcessEngine(t *testing.T) {
	env := harnesstest.NewEnv(t)
	t.Setenv("FAKERUNNER_FIXTURE", fixtures.Path("simple.jsonl"))
	t.Setenv("FAKERUNNER_CAPTURE", env.Capture)
	eng := process.New(uaharness.Config{RunnerPath: harnesstest.FakeRunner(t), StateDir: env.StateDir, KillGrace: time.Second, Getenv: env.Getenv})
	settings := session.Settings{Provider: "openai-codex", Model: "gpt-6-sol", Effort: "high", Workspace: env.Workspace}
	s, err := session.Open(context.Background(), eng, session.Options{Settings: settings, Shell: shellRunner(t)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	h := &harness{t: t, s: s}

	res, err := s.RunShell(t.Context(), "echo from the user")
	require.NoError(t, err)
	_, err = s.Submit("see above")
	require.NoError(t, err)
	h.until(isType[core.RunFinished])

	var req struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	stdin, err := os.ReadFile(filepath.Join(env.Capture, "stdin.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(stdin, &req))
	require.Len(t, req.Messages, 2)
	assert.Equal(t, res.Text(), req.Messages[0].Content)
	assert.Contains(t, req.Messages[0].Content, "Output:\nfrom the user\n")
	assert.Equal(t, "see above", req.Messages[1].Content)
}
