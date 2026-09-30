package session_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/session"
)

// afterTool starts a run whose model is responding and sends a message
// after its tool call (enter while the agent works).
func afterTool(t *testing.T, h *harness) (*fakeRun, core.UserInput) {
	t.Helper()
	_, err := h.s.Submit("work")
	require.NoError(t, err)
	run := h.nextRun()
	run.sink(core.TurnStarted{At: time.Now(), Turn: 1})
	h.until(isType[core.TurnStarted])
	in, err := h.s.Send("look here", session.SendAfterTool)
	require.NoError(t, err)
	q := h.until(func(e core.Event) bool { q, ok := e.(session.InputQueued); return ok && q.Input.ID == in.ID })
	assert.True(t, q.(session.InputQueued).AfterTool, "the TUI labels it")

	return run, in
}

func TestSession_SendAfterTool(t *testing.T) {
	t.Run("held through the response and its tool call, then steered", func(t *testing.T) {
		h := newHarness(t, fakeCaps{LiveInput: true})
		run, in := afterTool(t, h)
		run.sink(core.ModelResponded{At: time.Now(), Turn: 1})
		run.sink(core.ToolCalled{At: time.Now(), CallID: "c1", Name: "shell"})
		h.until(isType[core.ToolCalled])
		assert.Empty(t, run.sent, "the response and its tool call are not cut off")

		run.sink(core.ToolFinished{At: time.Now(), CallID: "c1", Name: "shell", OK: true})
		h.until(func(e core.Event) bool { d, ok := e.(session.InputDelivered); return ok && d.ID == in.ID })
		assert.Equal(t, []string{in.ID}, ids(run.sent), "it rides the next model request")
		run.finish(core.StatusOK)
		h.until(isType[session.Idle])
	})

	t.Run("the run ends first: it goes out with the next run", func(t *testing.T) {
		h := newHarness(t, fakeCaps{LiveInput: true})
		run, in := afterTool(t, h)
		run.sink(core.ModelResponded{At: time.Now(), Turn: 1})
		run.finish(core.StatusOK)
		next := h.nextRun()

		assert.Empty(t, run.sent)
		assert.Equal(t, []string{in.ID}, ids(next.req.Messages))
		next.finish(core.StatusOK)
		h.until(isType[session.Idle])
	})

	t.Run("withdrawn while held", func(t *testing.T) {
		h := newHarness(t, fakeCaps{LiveInput: true})
		run, in := afterTool(t, h)
		ok, err := h.s.Withdraw(in.ID)
		require.NoError(t, err)
		assert.True(t, ok)
		run.sink(core.ModelResponded{At: time.Now(), Turn: 1})
		run.finish(core.StatusOK)
		h.until(isType[session.Idle])
		assert.Empty(t, run.sent)
	})

	t.Run("no model response under way: sent now", func(t *testing.T) {
		h := newHarness(t, fakeCaps{LiveInput: true})
		_, err := h.s.Submit("work")
		require.NoError(t, err)
		run := h.nextRun()
		in, err := h.s.Send("look here", session.SendAfterTool)
		require.NoError(t, err)
		h.until(func(e core.Event) bool { d, ok := e.(session.InputDelivered); return ok && d.ID == in.ID })
		run.finish(core.StatusOK)
		h.until(isType[session.Idle])
	})
}
