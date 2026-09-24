package session_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/hooks"
	"github.com/viktordanov/uagent-harness/internal/session"
)

func ids(msgs []core.UserInput) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.ID)
	}

	return out
}

// queueTwo starts a run and queues two messages behind it.
func queueTwo(t *testing.T, h *harness) (*fakeRun, []string) {
	t.Helper()
	_, err := h.s.Submit("work")
	require.NoError(t, err)
	run := h.nextRun()
	var queued []string
	for _, text := range []string{"second", "third [Image #1]\n<uah-image label=\"[Image #1]\" ref=\"x.png\"/>"} {
		in, err := h.s.Submit(text)
		require.NoError(t, err)
		queued = append(queued, in.ID)
	}

	return run, queued
}

func TestSession_SteerQueued(t *testing.T) {
	t.Run("live input: the queue reaches the running agent in order", func(t *testing.T) {
		h := newHarness(t, engine.Capabilities{LiveInput: true})
		run, queued := queueTwo(t, h)
		h.until(isType[session.InputDelivered]) // "work"

		n, err := h.s.SteerQueued()
		require.NoError(t, err)
		first := h.until(isType[session.InputDelivered]).(session.InputDelivered)
		second := h.until(isType[session.InputDelivered]).(session.InputDelivered)

		assert.Equal(t, 2, n)
		assert.Equal(t, queued, ids(run.sent), "each message keeps its ID, in order")
		assert.Equal(t, "second", run.sent[0].Text)
		assert.Contains(t, run.sent[1].Text, `<uah-image label="[Image #1]"`, "and its images")
		assert.Equal(t, queued, []string{first.ID, second.ID})

		run.finish(core.StatusOK)
		h.until(isType[session.Idle])
		select {
		case r := <-h.eng.started:
			t.Fatalf("the queue was sent; no run should follow, got %v", texts(r.req.Messages))
		case <-time.After(100 * time.Millisecond):
		}
	})

	t.Run("live input while the run starts: sent once it has", func(t *testing.T) {
		h := newHarness(t, engine.Capabilities{LiveInput: true})
		h.eng.gate = make(chan struct{})
		_, err := h.s.Submit("work")
		require.NoError(t, err)
		b, err := h.s.Submit("b")
		require.NoError(t, err)
		c, err := h.s.Submit("c")
		require.NoError(t, err)

		_, err = h.s.SteerQueued()
		require.NoError(t, err)
		close(h.eng.gate)
		run := h.nextRun()
		h.until(func(e core.Event) bool { d, ok := e.(session.InputDelivered); return ok && d.ID == c.ID })

		assert.Equal(t, []string{"work"}, texts(run.req.Messages))
		assert.Equal(t, []string{b.ID, c.ID}, ids(run.sent))
		run.finish(core.StatusOK)
		h.until(isType[session.Idle])
	})

	t.Run("process engine: interrupt and restart with the queue", func(t *testing.T) {
		h := newHarness(t, engine.Capabilities{})
		first, queued := queueTwo(t, h)

		_, err := h.s.SteerQueued()
		require.NoError(t, err)
		next := h.nextRun()

		assert.Equal(t, core.StatusInterrupted, first.result.Status)
		assert.Equal(t, queued, ids(next.req.Messages))
		next.finish(core.StatusOK)
		h.until(isType[session.Idle])
	})

	t.Run("the queue an interrupt kept starts a run", func(t *testing.T) {
		h := newHarness(t, engine.Capabilities{LiveInput: true})
		_, queued := queueTwo(t, h)
		require.NoError(t, h.s.Interrupt())
		h.until(isType[session.Idle])

		n, err := h.s.SteerQueued()
		require.NoError(t, err)
		next := h.nextRun()

		assert.Equal(t, 2, n)
		assert.Equal(t, queued, ids(next.req.Messages))
		next.finish(core.StatusOK)
		h.until(isType[session.Idle])
	})

	t.Run("nothing queued: nothing happens", func(t *testing.T) {
		h := newHarness(t, engine.Capabilities{LiveInput: true})
		n, err := h.s.SteerQueued()
		require.NoError(t, err)
		assert.Zero(t, n)

		_, err = h.s.Submit("work")
		require.NoError(t, err)
		run := h.nextRun()
		n, err = h.s.SteerQueued()
		require.NoError(t, err)
		assert.Zero(t, n)
		assert.Empty(t, run.sent)
		run.finish(core.StatusOK)
		h.until(isType[session.Idle])
	})
}

func TestSession_SteerQueuedWhileHooksCheck(t *testing.T) {
	h := withHooks(t, hooks.Hook{Event: hooks.UserPromptSubmit, Command: "sleep 0.2"})
	_, err := h.s.Submit("work")
	require.NoError(t, err)
	first := h.nextRun()
	second, err := h.s.Submit("second")
	require.NoError(t, err)

	n, err := h.s.SteerQueued()
	require.NoError(t, err)
	next := h.nextRun()

	assert.Equal(t, 1, n)
	assert.Equal(t, core.StatusInterrupted, first.result.Status, "once its hooks allow it, the message goes as a steer")
	assert.Equal(t, []string{second.ID}, ids(next.req.Messages))
	next.finish(core.StatusOK)
}
