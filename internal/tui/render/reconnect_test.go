package render_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// TestReconnectingLine: the status line counts down to the next attempt,
// says "connecting" while it is in flight, and goes back to the work once
// the retries end; the detailed view shows it in the footer.
func TestReconnectingLine(t *testing.T) {
	s := apply(base(),
		core.RunStarted{At: t0, RunID: "r1"},
		core.TurnStarted{At: t0, Turn: 1},
		engine.Reconnecting{At: t0, Attempt: 3, MaxAttempts: 10, Delay: 8 * time.Second, Reason: "connection reset by peer"},
		state.Tick{Now: t0},
	)
	assert.Contains(t, screen(s, ""), "Reconnecting, attempt 3 of 10 (retrying in 8s • esc to interrupt)")
	assert.Contains(t, screen(apply(s, state.Tick{Now: t0.Add(2500 * time.Millisecond)}), ""), "(retrying in 6s • esc to interrupt)", "rounded up")
	assert.Contains(t, screen(apply(s, state.Tick{Now: t0.Add(9 * time.Second)}), ""), "Reconnecting, attempt 3 of 10 (connecting • esc to interrupt)")
	assert.Contains(t, screen(apply(s, state.ToggleDetails{}), ""), " Reconnecting, attempt 3 of 10 (retrying in 8s)")

	after := screen(apply(s, engine.ReconnectEnded{At: t0, OK: true}), "")
	assert.NotContains(t, after, "Reconnecting")
	assert.Contains(t, after, "Thinking")
}
