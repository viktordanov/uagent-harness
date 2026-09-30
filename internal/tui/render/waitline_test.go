package render_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uah/internal/engine"
	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/internal/tui/state"
)

// TestWaitLine: the status line names the wait, how long it has lasted and
// the run has run, and counts a retry down; the detailed view shows a
// retry or a stall in the footer.
func TestWaitLine(t *testing.T) {
	s := apply(base(),
		core.RunStarted{At: t0, RunID: "r1"},
		core.TurnStarted{At: t0.Add(60 * time.Second), Turn: 1},
		engine.ModelProgress{At: t0.Add(61 * time.Second), Phase: engine.PhaseStreaming, Tool: "apply_patch", Target: "internal/foo.go", ToolBytes: 4200},
		state.Tick{Now: t0.Add(72 * time.Second)},
	)
	assert.Contains(t, screen(s, ""), "Writing a patch · internal/foo.go · 4.2 kB (12s · 1m 12s • esc to interrupt)")

	retry := apply(s, engine.Reconnecting{At: t0.Add(72 * time.Second), Attempt: 3, MaxAttempts: 10, Delay: 8 * time.Second, Reason: "connection reset by peer"})
	assert.Contains(t, screen(retry, ""), "Reconnecting · connection reset by peer · attempt 3/10 · retry in ~8s (12s · 1m 12s")
	assert.Contains(t, screen(apply(retry, state.Tick{Now: t0.Add(74500 * time.Millisecond)}), ""), "retry in ~6s", "rounded up")
	assert.Contains(t, screen(apply(retry, state.Tick{Now: t0.Add(81 * time.Second)}), ""), "attempt 3/10 · connecting")
	assert.Contains(t, screen(apply(retry, state.ToggleDetails{}), ""), " Reconnecting · connection reset by peer · attempt 3/10 · retry in ~8s")

	after := screen(apply(retry, engine.ReconnectEnded{At: t0, OK: true}), "")
	assert.NotContains(t, after, "Reconnecting")
	assert.Contains(t, after, "Writing a patch", "back to the request's progress")

	stopping := apply(s, session.InputQueued{At: t0}, state.Esc{}, state.Esc{}, state.Tick{Now: t0.Add(75 * time.Second)})
	assert.Contains(t, screen(stopping, ""), "Stopping (3s · 1m 15s • esc again to force)")
}
