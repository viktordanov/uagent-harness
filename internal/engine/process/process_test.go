package process_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
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
	"github.com/viktordanov/uagent-harness/internal/sandbox"
	"github.com/viktordanov/uagent-harness/testing/harnesstest"
)

// sink collects a run's events.
type sink struct {
	mu     sync.Mutex
	events []core.Event
}

func (s *sink) add(e core.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}

func request(env *harnesstest.Env, text string) core.Request {
	return core.Request{
		SessionID: "3f2a1b2c-0000-4000-8000-000000000000", Provider: "openai-codex", Model: "gpt-6-sol", Effort: "high",
		Workspace: env.Workspace, Messages: []core.UserInput{{ID: "m1", Text: text}},
	}
}

// TestProcess_RunsTheRunner starts the fake runner through uagent and
// reports its events and result; nothing reaches a live run.
func TestProcess_RunsTheRunner(t *testing.T) {
	env := harnesstest.NewEnv(t)
	t.Setenv("FAKERUNNER_FIXTURE", fixtures.Path("simple.jsonl"))
	eng := process.New(uaharness.Config{RunnerPath: harnesstest.FakeRunner(t), StateDir: env.StateDir, Getenv: env.Getenv, KillGrace: time.Second})
	assert.Equal(t, "process", eng.Name())
	assert.Equal(t, engine.Capabilities{}, eng.Capabilities(), "no live input, settings, compaction, modes, or rules")
	assert.Len(t, eng.Capabilities().Lacks(), len(engine.Table), "it lacks every feature in the capability table")

	var got sink
	run, err := eng.Start(context.Background(), request(env, "hi"), engine.Options{}, got.add)
	require.NoError(t, err)
	for name, set := range map[string]error{
		"Send": run.Send(core.UserInput{ID: "m2", Text: "more"}), "SetEffort": run.SetEffort("low"),
		"SetModel": run.SetModel("gpt-6-luna"), "SetServiceTier": run.SetServiceTier("priority"),
		"SetMode": run.SetMode(approval.ModeReadOnly), "Compact": run.Compact(""), "Clear": run.Clear(),
	} {
		assert.ErrorIs(t, set, engine.ErrUnsupported, name)
	}
	result, err := run.Wait()
	require.NoError(t, err)
	assert.Equal(t, core.StatusOK, result.Status)
	assert.NotEmpty(t, result.Answer)

	got.mu.Lock()
	defer got.mu.Unlock()
	require.NotEmpty(t, got.events)
	assert.IsType(t, core.RunStarted{}, got.events[0])
	assert.IsType(t, core.RunFinished{}, got.events[len(got.events)-1])
}

// TestProcess_Interrupt stops a run whose tool hangs.
func TestProcess_Interrupt(t *testing.T) {
	env := harnesstest.NewEnv(t)
	t.Setenv("FAKERUNNER_FIXTURE", fixtures.Path("timeout.jsonl"))
	t.Setenv("FAKERUNNER_HANG", "1")
	t.Setenv("FAKERUNNER_CAPTURE", env.Capture)
	eng := process.New(uaharness.Config{RunnerPath: harnesstest.FakeRunner(t), StateDir: env.StateDir, Getenv: env.Getenv, KillGrace: 300 * time.Millisecond})
	run, err := eng.Start(context.Background(), request(env, "start"), engine.Options{}, func(core.Event) {})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(env.Capture, "hung.pids"))
		return err == nil
	}, 10*time.Second, 10*time.Millisecond, "the tool is running")
	run.Interrupt()
	result, err := run.Wait()
	require.NoError(t, err)
	assert.Equal(t, core.StatusInterrupted, result.Status)
}

// TestProcess_SandboxedPerMode builds one harness per permission mode's
// sandbox, when a run first asks for it, and gives the runner that mode's
// shell: the process engine's permission modes apply from the next run.
func TestProcess_SandboxedPerMode(t *testing.T) {
	env := harnesstest.NewEnv(t)
	t.Setenv("FAKERUNNER_FIXTURE", fixtures.Path("simple.jsonl"))
	t.Setenv("FAKERUNNER_CAPTURE", env.Capture)
	var mu sync.Mutex
	built := map[sandbox.Mode]int{}
	eng, err := process.NewSandboxed(sandbox.WorkspaceWrite, false, func(mode sandbox.Mode) (uaharness.Config, error) {
		mu.Lock()
		built[mode]++
		mu.Unlock()
		backend := uaharness.RunnerBackend{Path: harnesstest.FakeRunner(t), Env: []string{"SHELL=/shell-for-" + string(mode)}}

		return uaharness.Config{Backend: backend, StateDir: env.StateDir, Getenv: env.Getenv, KillGrace: time.Second}, nil
	})
	require.NoError(t, err)

	shellOf := func(mode approval.Mode) string {
		t.Helper()
		run, err := eng.Start(context.Background(), request(env, "hi"), engine.Options{Mode: mode}, func(core.Event) {})
		require.NoError(t, err)
		_, err = run.Wait()
		require.NoError(t, err)
		env, err := os.ReadFile(filepath.Join(env.Capture, "env.txt"))
		require.NoError(t, err)

		return string(env)
	}
	assert.Contains(t, shellOf(""), "SHELL=/shell-for-workspace-write", "no mode: the engine's own")
	assert.Contains(t, shellOf(approval.ModeReadOnly), "SHELL=/shell-for-read-only")
	assert.Contains(t, shellOf(approval.ModeReadOnly), "SHELL=/shell-for-read-only")
	assert.Contains(t, shellOf(approval.ModeAuto), "SHELL=/shell-for-workspace-write", "auto runs in the workspace sandbox")
	assert.Equal(t, map[sandbox.Mode]int{sandbox.WorkspaceWrite: 1, sandbox.ReadOnly: 1}, built, "each sandbox is built once")

	failing, err := process.NewSandboxed(sandbox.WorkspaceWrite, false, func(mode sandbox.Mode) (uaharness.Config, error) {
		if mode == sandbox.ReadOnly {
			return uaharness.Config{}, errors.New("no seatbelt")
		}

		return uaharness.Config{RunnerPath: harnesstest.FakeRunner(t), StateDir: env.StateDir, Getenv: env.Getenv}, nil
	})
	require.NoError(t, err)
	_, err = failing.Start(context.Background(), request(env, "hi"), engine.Options{Mode: approval.ModeReadOnly}, func(core.Event) {})
	require.ErrorContains(t, err, "failed to set up the read-only sandbox: no seatbelt")
}
