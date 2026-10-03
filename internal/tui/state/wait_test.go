package state_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uah/internal/compaction"
	"github.com/viktordanov/uah/internal/engine"
	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/internal/tui/state"
)

// TestCurrentWait: the status line names what the run waits on, the most
// pressing wait first.
func TestCurrentWait(t *testing.T) {
	at := func(d time.Duration) time.Time { return t0.Add(d) }
	turn := []any{core.RunStarted{At: t0, RunID: "r1"}, core.TurnStarted{At: at(time.Second), Turn: 1}}
	cases := map[string]struct {
		events []any
		want   state.Wait
	}{
		"a run before anything":       {events: []any{core.RunStarted{At: t0, RunID: "r1"}}, want: state.Wait{What: "Working"}},
		"the model, without progress": {events: turn, want: state.Wait{What: "Thinking", Since: at(time.Second)}},
		"sending": {
			events: append(turn, engine.ModelProgress{At: at(2 * time.Second), Phase: engine.PhaseSending, Bytes: 1_234_567}),
			want:   state.Wait{What: "Sending the request · 1.2 MB", Since: at(time.Second)},
		},
		"waiting": {
			events: append(turn, engine.ModelProgress{At: at(2 * time.Second), Phase: engine.PhaseWaiting}),
			want:   state.Wait{What: "Waiting for the model", Since: at(time.Second)},
		},
		"writing a patch": {
			events: append(turn, engine.ModelProgress{At: at(2 * time.Second), Phase: engine.PhaseStreaming, Tool: "apply_patch", Target: "internal/foo.go", ToolBytes: 4200}),
			want:   state.Wait{What: "Writing a patch", Path: "internal/foo.go", Detail: "4.2 kB", Since: at(time.Second)},
		},
		"preparing another tool": {
			events: append(turn, engine.ModelProgress{At: at(2 * time.Second), Phase: engine.PhaseStreaming, Tool: "Bash", ToolBytes: 1100}),
			want:   state.Wait{What: "Preparing Bash · 1.1 kB", Since: at(time.Second)},
		},
		"a stall": {
			events: append(turn, engine.ModelProgress{At: at(2 * time.Second), Phase: engine.PhaseStreaming}, state.Tick{Now: at(47 * time.Second)}),
			want:   state.Wait{What: "Thinking", Detail: "no data for 45s", Since: at(time.Second), Warn: true},
		},
		"a retry": {
			events: append(turn, engine.Reconnecting{At: at(2 * time.Second), Attempt: 3, MaxAttempts: 10, Delay: 8 * time.Second, Reason: "server_is_overloaded: slow down"}, state.Tick{Now: at(2 * time.Second)}),
			want:   state.Wait{What: "Reconnecting · server_is_overloaded: slow down · attempt 3/10 · retry in ~8s", Since: at(time.Second), Warn: true},
		},
		"a retry in flight": {
			events: append(turn, engine.Reconnecting{At: at(2 * time.Second), Attempt: 3, MaxAttempts: 10, Delay: 8 * time.Second, Reason: "EOF"}, state.Tick{Now: at(11 * time.Second)}),
			want:   state.Wait{What: "Reconnecting · EOF · attempt 3/10 · connecting", Since: at(time.Second), Warn: true},
		},
		"offline": {
			events: append(turn, engine.Reconnecting{At: at(2 * time.Second), Attempt: 1, MaxAttempts: 10, Delay: 5 * time.Second, Reason: "waiting for network: no route to host", Offline: true}, state.Tick{Now: at(2 * time.Second)}),
			want:   state.Wait{What: "Reconnecting · waiting for network: no route to host · attempt 1/10 · retry in ~5s", Since: at(time.Second), Warn: true},
		},
		"the retries end": {
			events: append(turn, engine.Reconnecting{At: at(2 * time.Second), Attempt: 2, MaxAttempts: 10}, engine.ReconnectEnded{At: at(3 * time.Second), OK: true}),
			want:   state.Wait{What: "Thinking", Since: at(time.Second)},
		},
		"compacting": {
			events: append(turn, engine.CompactionStarted{At: at(2 * time.Second), Trigger: compaction.TriggerAuto}),
			want:   state.Wait{What: "Compacting the context", Since: at(2 * time.Second)},
		},
		"auto-review": {
			events: append(turn, core.ModelResponded{At: at(2 * time.Second), Turn: 1}, engine.AutoReviewing{At: at(3 * time.Second), Command: "rm -rf build"}),
			want:   state.Wait{What: "Auto-reviewing the command · rm -rf build", Since: at(3 * time.Second)},
		},
		"a hook": {
			events: append(turn, core.ModelResponded{At: at(2 * time.Second), Turn: 1}, session.HookRan{At: at(3 * time.Second), Event: "PreToolUse", Command: "./check.sh", Outcome: "running"}),
			want:   state.Wait{What: "Running PreToolUse hook · ./check.sh", Since: at(3 * time.Second)},
		},
		"a hook that ended": {
			events: append(turn, core.ModelResponded{At: at(2 * time.Second), Turn: 1}, session.HookRan{At: at(3 * time.Second), Event: "PreToolUse", Outcome: "running"}, session.HookRan{At: at(4 * time.Second), Event: "PreToolUse", Outcome: "ok"}),
			want:   state.Wait{What: "Working"},
		},
		"an approval over the hook": {
			events: append(turn, session.HookRan{At: at(3 * time.Second), Event: "PermissionRequest", Outcome: "running"}, session.ApprovalRequested{At: at(4 * time.Second), ID: "a1", Command: "git push"}),
			want:   state.Wait{What: "Waiting for approval · git push", Since: at(4 * time.Second)},
		},
		"a call not started": {
			events: append(turn, core.ModelResponded{At: at(2 * time.Second), Turn: 1}, core.ToolCalled{At: at(2 * time.Second), CallID: "c1", Name: "Bash", Label: "ls"}),
			want:   state.Wait{What: "Preparing Bash"},
		},
		"commands": {
			events: append(turn, core.ModelResponded{At: at(2 * time.Second), Turn: 1},
				core.ToolCalled{At: at(2 * time.Second), CallID: "c1", Name: "Bash"}, core.ToolStarted{At: at(2 * time.Second), CallID: "c1", OpID: "o1"},
				core.ToolCalled{At: at(2 * time.Second), CallID: "c2", Name: "Bash"}, core.ToolStarted{At: at(2 * time.Second), CallID: "c2", OpID: "o2"}),
			want: state.Wait{What: "Running 2 commands"},
		},
		"agents": {
			events: append(turn, core.ModelResponded{At: at(2 * time.Second), Turn: 1},
				core.ToolCalled{At: at(2 * time.Second), CallID: "c1", Name: "wait_agent"}, core.ToolStarted{At: at(3 * time.Second), CallID: "c1", OpID: "o1"}),
			want: state.Wait{What: "Waiting for agents", Since: at(3 * time.Second)},
		},
		"an MCP call": {
			events: append(turn, core.ModelResponded{At: at(2 * time.Second), Turn: 1},
				core.ToolCalled{At: at(2 * time.Second), CallID: "c1", Name: "mcp__docs__search"}, core.ToolStarted{At: at(3 * time.Second), CallID: "c1", OpID: "o1"}),
			want: state.Wait{What: "Calling server tool · docs__search", Since: at(3 * time.Second)},
		},
		"stopping over everything": {
			events: append(turn, session.InputQueued{At: t0}, engine.CompactionStarted{At: at(2 * time.Second)}, state.Tick{Now: at(3 * time.Second)}, state.Esc{}, state.Esc{}),
			want:   state.Wait{What: "Stopping", Hint: "esc again to force", Since: at(3 * time.Second)},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s, _ := apply(opened(), c.events...)
			got, ok := s.CurrentWait()
			require.True(t, ok)
			assert.Equal(t, c.want, got)
		})
	}

	_, ok := opened().CurrentWait()
	assert.False(t, ok, "no run, no wait")
	idle, _ := apply(opened(), engine.Reconnecting{At: t0, Attempt: 2, MaxAttempts: 10}, engine.ModelProgress{At: t0, Phase: engine.PhaseSending})
	assert.Nil(t, idle.Live, "events without a run leave nothing behind")
}

// TestCurrentWait_Phases: a turn's first moments and the hand-over between
// the phases, as the engine reports them, also through a retry.
func TestCurrentWait_Phases(t *testing.T) {
	s, _ := apply(opened(), core.RunStarted{At: t0, RunID: "r1"}, core.TurnStarted{At: t0, Turn: 1})
	steps := []struct {
		ev   any
		want string
	}{
		{state.Tick{Now: t0}, "Thinking"}, // before any progress
		{engine.ModelProgress{At: t0, Phase: engine.PhaseConnecting, Attempt: 1}, "Waiting for the model"},
		{engine.ModelProgress{At: t0, Phase: engine.PhaseSending, Attempt: 1, Bytes: 84_000}, "Sending the request · 84 kB"},
		{engine.ModelProgress{At: t0, Phase: engine.PhaseWaiting, Attempt: 1}, "Waiting for the model"},
		{engine.ModelProgress{At: t0, Phase: engine.PhaseStreaming, Attempt: 1}, "Thinking"},
		{engine.ModelProgress{At: t0, Phase: engine.PhaseStreaming, Attempt: 1, Tool: "apply_patch", Target: "a.go", ToolBytes: 900}, "Writing a patch · a.go · 900 B"},
		{engine.Reconnecting{At: t0, Attempt: 2, MaxAttempts: 10, Reason: "EOF"}, "Reconnecting · EOF · attempt 2/10 · connecting"},
		{engine.ModelProgress{At: t0, Phase: engine.PhaseConnecting, Attempt: 2}, "Reconnecting · EOF · attempt 2/10 · connecting"},
		{engine.ModelProgress{At: t0, Phase: engine.PhaseWaiting, Attempt: 2}, "Reconnecting · EOF · attempt 2/10 · connecting"},
		{engine.ModelProgress{At: t0, Phase: engine.PhaseStreaming, Attempt: 2}, "Thinking"},
		{engine.ModelProgress{At: t0, Phase: engine.PhaseDone, Attempt: 2}, "Working"},
	}
	for _, step := range steps {
		s, _ = apply(s, step.ev)
		got, _ := s.CurrentWait()
		assert.Equal(t, step.want, got.Text(), "after %T %+v", step.ev, step.ev)
	}
}

// TestReduce_ModelRequestEnds: the model's wait ends with the runner's
// response or the request's done phase, whichever comes first, and each
// retry leaves a detail line.
func TestReduce_ModelRequestEnds(t *testing.T) {
	s, _ := apply(opened(), core.RunStarted{At: t0, RunID: "r1"}, core.TurnStarted{At: t0, Turn: 1},
		engine.Reconnecting{At: t0, Attempt: 3, MaxAttempts: 10, Reason: "connection reset by peer"})
	last := s.Items[len(s.Items)-1]
	assert.Equal(t, state.LevelDebug, last.Level)
	assert.Equal(t, "reconnecting, attempt 3 of 10: connection reset by peer", last.Text)

	for name, ev := range map[string]core.Event{
		"response": core.ModelResponded{At: t0, Turn: 1},
		"done":     engine.ModelProgress{At: t0, Phase: engine.PhaseDone},
	} {
		ended, _ := apply(s, ev)
		got, _ := ended.CurrentWait()
		assert.Equal(t, state.Wait{What: "Working"}, got, name)
	}
	late, _ := apply(s, core.ModelResponded{At: t0, Turn: 1}, engine.ModelProgress{At: t0, Phase: engine.PhaseDone})
	got, _ := late.CurrentWait()
	assert.Equal(t, state.Wait{What: "Working"}, got, "a done phase after the response starts nothing")

	finished, _ := apply(s, core.RunFinished{At: t0, Result: core.Result{Request: core.Request{RunID: "r1"}, Status: core.StatusFailed}})
	assert.Nil(t, finished.Live, "the run gave up")
}

// TestReduce_EscWhileStopping: esc esc interrupts and the run shows as
// stopping; then a single esc forces the stop. /stop interrupts the same way.
func TestReduce_EscWhileStopping(t *testing.T) {
	busy, _ := apply(opened(), session.InputQueued{At: t0, Input: core.UserInput{ID: "a", Text: "go"}}, core.RunStarted{At: t0, RunID: "r1"})

	s, effects := apply(busy, state.Esc{}, state.Esc{})
	assert.Equal(t, []state.Effect{state.EffInterrupt{}}, effects)
	assert.Equal(t, t0, s.Live.Stopping)

	s, effects = apply(s, state.Tick{Now: t0.Add(10 * time.Second)}, state.Esc{})
	assert.Equal(t, []state.Effect{state.EffInterrupt{}}, effects, "one esc forces the stop")
	assert.Equal(t, "forcing the stop…", s.Status)
	s, _ = apply(s, state.Tick{Now: t0.Add(13 * time.Second)})
	assert.Empty(t, s.Status, "the notice expires")

	s, effects = apply(busy, state.Submit{Text: "/stop"})
	assert.Equal(t, []state.Effect{state.EffInterrupt{}}, effects)
	assert.Equal(t, t0, s.Live.Stopping)
}

// TestStatus_ShowsTheWait: /status says what the live run waits on.
func TestStatus_ShowsTheWait(t *testing.T) {
	s, _ := apply(opened(), core.RunStarted{At: t0, RunID: "r1"}, core.TurnStarted{At: t0, Turn: 1},
		engine.ModelProgress{At: t0, Phase: engine.PhaseWaiting}, state.Submit{Text: "/status"})
	var texts []string
	for _, it := range s.Items {
		texts = append(texts, it.Text)
	}
	assert.Contains(t, texts, "now: Waiting for the model · waiting")
}

// TestCurrentWait_ShortPaths: the file a patch names and the command an
// approval, a hook or an auto-review shows are relative to the workspace,
// and under the home directory after ~, as the tool lines show them.
func TestCurrentWait_ShortPaths(t *testing.T) {
	turn := []any{core.RunStarted{At: t0, RunID: "r1"}, core.TurnStarted{At: t0, Turn: 1}}
	for name, c := range map[string]struct {
		ev   any
		want string
	}{
		"a patch in the workspace":  {engine.ModelProgress{At: t0, Phase: engine.PhaseStreaming, Tool: "apply_patch", Target: "/workspace/internal/foo.go", ToolBytes: 900}, "Writing a patch · internal/foo.go · 900 B"},
		"a patch under home":        {engine.ModelProgress{At: t0, Phase: engine.PhaseStreaming, Tool: "apply_patch", Target: "/home/ada/notes/a.md", ToolBytes: 900}, "Writing a patch · ~/notes/a.md · 900 B"},
		"another user's home":       {engine.ModelProgress{At: t0, Phase: engine.PhaseStreaming, Tool: "apply_patch", Target: "/home/adam/a.md", ToolBytes: 900}, "Writing a patch · /home/adam/a.md · 900 B"},
		"an approval":               {session.ApprovalRequested{At: t0, ID: "a1", Command: "cat /home/ada/notes/a.md\n  /workspace/b.go"}, "Waiting for approval · cat ~/notes/a.md b.go"},
		"a hook":                    {session.HookRan{At: t0, Event: "PreToolUse", Command: "/home/ada/bin/check.sh", Outcome: "running"}, "Running PreToolUse hook · ~/bin/check.sh"},
		"an auto-review of a write": {engine.AutoReviewing{At: t0, Command: "rm -rf /workspace/build"}, "Auto-reviewing the command · rm -rf build"},
	} {
		s := opened()
		s.Home = "/home/ada"
		got, _ := apply(s, append(turn, c.ev)...)
		w, _ := got.CurrentWait()
		assert.Equal(t, c.want, w.Text(), name)
	}
}
