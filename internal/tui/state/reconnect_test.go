package state_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// TestReduce_Reconnecting: a retry shows until the retries end, a response
// arrives, or the run finishes, and each attempt leaves a detail line.
func TestReduce_Reconnecting(t *testing.T) {
	live, _ := apply(opened(), core.RunStarted{At: t0, RunID: "r1"}, core.TurnStarted{At: t0, Turn: 1})
	s, _ := apply(live, engine.Reconnecting{At: t0, Attempt: 3, MaxAttempts: 10, Delay: 8 * time.Second, Reason: "connection reset by peer"})
	require.NotNil(t, s.Live.Reconnect)
	assert.Equal(t, state.Reconnect{Attempt: 3, MaxAttempts: 10, Retry: t0.Add(8 * time.Second), Reason: "connection reset by peer"}, *s.Live.Reconnect)
	last := s.Items[len(s.Items)-1]
	assert.Equal(t, state.LevelDebug, last.Level)
	assert.Equal(t, "reconnecting, attempt 3 of 10: connection reset by peer", last.Text)

	ended, _ := apply(s, engine.ReconnectEnded{At: t0, OK: true})
	assert.Nil(t, ended.Live.Reconnect, "a response arrived")

	responded, _ := apply(s, core.ModelResponded{At: t0, Turn: 1})
	assert.Nil(t, responded.Live.Reconnect)

	finished, _ := apply(s, core.RunFinished{At: t0, Result: core.Result{Request: core.Request{RunID: "r1"}, Status: core.StatusFailed}})
	assert.Nil(t, finished.Live, "the run gave up")

	idle, _ := apply(opened(), engine.Reconnecting{At: t0, Attempt: 2, MaxAttempts: 10})
	assert.Nil(t, idle.Live, "no run, nothing to show")
}
