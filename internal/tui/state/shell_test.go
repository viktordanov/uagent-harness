package state_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"
	uaharness "github.com/viktordanov/uagent/harness"

	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
	"github.com/viktordanov/uagent-harness/internal/usershell"
)

func TestReduce_ShellMode(t *testing.T) {
	s, effects := apply(opened(), state.EnterShell{})
	require.True(t, s.Shell, "! on an empty composer")
	assert.Empty(t, effects)

	t.Run("enter runs the line as a command and leaves shell mode", func(t *testing.T) {
		got, effects := apply(s, state.Submit{Text: "  go test ./...  "})

		assert.Equal(t, []state.Effect{state.EffShell{Command: "go test ./..."}}, effects)
		assert.False(t, got.Shell)
	})

	t.Run("a slash is a path, not a command", func(t *testing.T) {
		assert.Empty(t, s.Suggestions("/bin/ls"), "no menu in shell mode")
		_, effects := apply(s, state.Submit{Text: "/bin/ls"})

		assert.Equal(t, []state.Effect{state.EffShell{Command: "/bin/ls"}}, effects)
	})

	t.Run("ctrl+enter runs it too", func(t *testing.T) {
		_, effects := apply(s, state.Steer{Text: "ls"})

		assert.Equal(t, []state.Effect{state.EffShell{Command: "ls"}}, effects)
	})

	t.Run("an empty line stays in shell mode", func(t *testing.T) {
		got, effects := apply(s, state.Submit{Text: "  "})

		assert.Empty(t, effects)
		assert.True(t, got.Shell)
	})

	t.Run("backspace or esc on the empty composer leaves", func(t *testing.T) {
		got, _ := apply(s, state.LeaveShell{})
		assert.False(t, got.Shell)
		got, effects := apply(s, state.Esc{})
		assert.False(t, got.Shell)
		assert.Empty(t, effects)
	})

	t.Run("a second ! is text", func(t *testing.T) {
		got, effects := apply(s, state.EnterShell{})

		assert.True(t, got.Shell)
		assert.Equal(t, []state.Effect{state.EffSetDraft{Text: "!"}}, effects)
	})

	t.Run("outside shell mode, backspace does nothing and enter sends", func(t *testing.T) {
		got, effects := apply(opened(), state.LeaveShell{}, state.Submit{Text: "ls"})

		assert.False(t, got.Shell)
		assert.Equal(t, []state.Effect{state.EffSubmit{Text: "ls"}}, effects)
	})

	t.Run("ctrl+n starts a new session from shell mode", func(t *testing.T) {
		got, effects := apply(s, state.NewSession{})

		assert.False(t, got.Shell)
		assert.Equal(t, []state.Effect{state.EffOpenSession{}}, effects)
	})
}

func TestReduce_ShellCommandItem(t *testing.T) {
	result := usershell.Result{Record: usershell.Record{Command: "make", ExitCode: 2, Duration: time.Second, Output: "error\n"}}
	s, _ := apply(opened(),
		session.ShellStarted{At: t0, ID: "sh-1", Command: "make"},
		session.ShellOutput{At: t0, ID: "sh-1", Text: "build"},
		session.ShellOutput{At: t0, ID: "sh-1", Text: "ing\n"},
	)
	it, ok := s.Item("msg:sh-1")
	require.True(t, ok)
	assert.Equal(t, state.KindShell, it.Kind)
	assert.Equal(t, state.ToolRunning, it.Tool)
	assert.Equal(t, "building\n", it.Detail, "output streams in")
	assert.True(t, it.Live())
	assert.True(t, s.ShellRunning())

	_, effects := apply(s, state.Esc{}, state.Esc{})
	assert.Equal(t, []state.Effect{state.EffInterrupt{}}, effects, "esc esc stops a running command, as it stops the agent")

	s, _ = apply(s, session.ShellFinished{At: t0.Add(time.Second), ID: "sh-1", Result: result})
	it, _ = s.Item("msg:sh-1")
	assert.Equal(t, state.ToolFailed, it.Tool)
	assert.Equal(t, 2, it.Exit)
	assert.Equal(t, "error\n", it.Detail, "the output the agent sees")
	assert.Equal(t, state.InputQueued, it.Input, "the agent has not seen it yet")
	assert.False(t, s.ShellRunning())

	s, _ = apply(s, core.UserMessage{At: t0, ID: "sh-1", Text: result.Text()})
	it, _ = s.Item("msg:sh-1")
	assert.Equal(t, state.InputDelivered, it.Input, "the runner's echo of the record")
	assert.Len(t, s.Items, 1, "the echo updates the item in place")
}

// TestReduce_ShellCommandResumed shows a command from a saved run as a
// command, not as a user message with tags.
func TestReduce_ShellCommandResumed(t *testing.T) {
	record := usershell.Record{Command: "git status", ExitCode: 0, Duration: 1500 * time.Millisecond, Output: "clean\n"}
	history := []session.LoadedRun{{
		Record: uaharness.RunRecord{Complete: true, Result: core.Result{
			Request: core.Request{RunID: "old-run", SessionID: "sess-2"}, Status: core.StatusOK, StartedAt: t0, Wall: time.Second,
		}},
		Events: []core.Event{
			core.UserMessage{At: t0, ID: "sh-9", Text: record.Text()},
			core.UserMessage{At: t0, ID: "m1", Text: "anything to commit?"},
		},
	}}

	s, _ := apply(opened(), state.HistoryLoaded{SessionID: "sess-2", Runs: history})

	assert.Equal(t, []state.Kind{state.KindRun, state.KindShell, state.KindUser, state.KindFinish}, kinds(s))
	it, _ := s.Item("msg:sh-9")
	assert.Equal(t, "git status", it.Text)
	assert.Equal(t, "clean\n", it.Detail)
	assert.Equal(t, state.ToolOK, it.Tool)
	assert.Equal(t, 1500*time.Millisecond, it.Duration)
	assert.Equal(t, state.InputDelivered, it.Input)
}
