package render_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// TestBacktrackShowsTheSelectedMessage: going back marks the selected
// message and scrolls the transcript to it.
func TestBacktrackShowsTheSelectedMessage(t *testing.T) {
	s := base()
	s.Caps.Rewind = true
	for i := 1; i <= 8; i++ {
		run := fmt.Sprintf("run-%d", i)
		s = apply(s,
			core.RunStarted{At: t0, RunID: run},
			core.UserMessage{At: t0, ID: fmt.Sprintf("m%d", i), Text: fmt.Sprintf("message %d", i)},
			core.AssistantMessage{At: t0, Text: fmt.Sprintf("answer %d", i), Final: true},
			core.RunFinished{At: t0, Result: core.Result{Request: core.Request{RunID: run}, Status: core.StatusOK}},
			session.Idle{At: t0},
		)
	}
	s = apply(s, state.Esc{Empty: true}, state.Esc{Empty: true})
	golden(t, "backtrack-latest", screen(s, ""))
	older := screen(apply(s, state.BacktrackMove{Delta: -6}), "")
	golden(t, "backtrack-older", older)
	assert.Contains(t, older, "▶ message 2")
	assert.NotContains(t, older, "message 8", "the transcript scrolls to the selected message")
}
