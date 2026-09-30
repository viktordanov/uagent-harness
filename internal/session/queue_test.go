package session_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/session"
)

// queuedOnDisk waits until the sidecar keeps want as the session's queue.
func queuedOnDisk(t *testing.T, dir, id string, want ...string) {
	t.Helper()
	assert.Eventually(t, func() bool {
		sc, _, err := session.ReadSidecar(dir, id)
		return err == nil && slices.Equal(want, sc.Queued)
	}, 5*time.Second, 5*time.Millisecond, "the sidecar keeps %q", want)
}

// TestSession_KeepsTheQueueAcrossRestarts pins that the messages waiting
// for a run are in the sidecar whenever they change, that a close keeps
// them, and that resuming queues them again, in order and unsent, until
// the next message; a new session starts with none.
func TestSession_KeepsTheQueueAcrossRestarts(t *testing.T) {
	dir := t.TempDir()
	eng := newFakeEngine(fakeCaps{LiveInput: true})
	opts := session.Options{Settings: settings(), SessionsDir: dir, Source: session.SourceTUI}
	s, err := session.Open(context.Background(), eng, opts)
	require.NoError(t, err)
	h := &harness{t: t, eng: eng, s: s}
	_, err = s.Submit("work")
	require.NoError(t, err)
	run := h.nextRun()
	run.sink(core.TurnStarted{At: time.Now(), Turn: 1})
	h.until(isType[core.TurnStarted])
	first, err := s.Submit("first")
	require.NoError(t, err)
	_, err = s.Send("second", session.SendAfterTool)
	require.NoError(t, err)
	third, err := s.Submit("third")
	require.NoError(t, err)
	queuedOnDisk(t, dir, s.ID(), "first", "second", "third")

	_, err = s.Withdraw(third.ID)
	require.NoError(t, err)
	queuedOnDisk(t, dir, s.ID(), "first", "second")
	require.NoError(t, s.Close())
	queuedOnDisk(t, dir, s.ID(), "first", "second")
	assert.Empty(t, run.sent, "nothing went out on the way down")

	opts.ID, opts.Resumed = s.ID(), true
	resumed, err := session.Open(context.Background(), eng, opts)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resumed.Close() })
	h = &harness{t: t, eng: eng, s: resumed}
	h.until(isType[session.Idle])
	var queued []string
	for _, e := range h.events {
		if q, ok := e.(session.InputQueued); ok {
			assert.False(t, q.AfterTool, "no run to hold it for")
			assert.NotEqual(t, first.ID, q.Input.ID)
			queued = append(queued, q.Input.Text)
		}
	}
	assert.Equal(t, []string{"first", "second"}, queued, "shown as queued again")
	select {
	case <-eng.started:
		t.Fatal("a resumed queue is not sent by itself")
	case <-time.After(50 * time.Millisecond):
	}

	_, err = resumed.Submit("next")
	require.NoError(t, err)
	assert.Equal(t, []string{"first", "second", "next"}, texts(h.nextRun().req.Messages))
	queuedOnDisk(t, dir, resumed.ID())

	fresh, err := session.Open(context.Background(), eng, session.Options{Settings: settings(), SessionsDir: dir, Source: session.SourceTUI})
	require.NoError(t, err)
	t.Cleanup(func() { _ = fresh.Close() })
	h = &harness{t: t, eng: eng, s: fresh}
	h.until(isType[session.SessionOpened])
	_, err = fresh.Submit("hello")
	require.NoError(t, err)
	assert.Equal(t, []string{"hello"}, texts(h.nextRun().req.Messages), "a new session queues nothing of another's")
}
