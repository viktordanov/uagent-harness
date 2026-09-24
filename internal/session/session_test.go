package session_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/session"
)

// fakeEngine scripts runs: each run echoes its messages (unless noEcho) and
// ends when the test finishes it or it is interrupted.
type fakeEngine struct {
	// gate, when set, holds Start until it is closed.
	gate     chan struct{}
	caps     engine.Capabilities
	noEcho   bool
	startErr error
	started  chan *fakeRun
}

func newFakeEngine(caps engine.Capabilities) *fakeEngine {
	return &fakeEngine{caps: caps, started: make(chan *fakeRun, 16)}
}

func (e *fakeEngine) Name() string                      { return "fake" }
func (e *fakeEngine) Capabilities() engine.Capabilities { return e.caps }

func (e *fakeEngine) Start(_ context.Context, req core.Request, _ engine.Options, sink core.Sink) (engine.Run, error) {
	if e.gate != nil {
		<-e.gate
	}
	if e.startErr != nil {
		return nil, e.startErr
	}
	r := &fakeRun{req: req, sink: sink, caps: e.caps, end: make(chan core.Status, 1), done: make(chan struct{})}
	sink(core.RunStarted{At: time.Now(), RunID: fmt.Sprintf("run-%d", len(e.started)), SessionID: req.SessionID})
	if !e.noEcho {
		for _, m := range req.Messages {
			sink(core.UserMessage{At: time.Now(), ID: m.ID, Text: m.Text})
		}
	}
	go r.wait()
	e.started <- r

	return r, nil
}

type fakeRun struct {
	req    core.Request
	sink   core.Sink
	caps   engine.Capabilities
	end    chan core.Status
	done   chan struct{}
	once   sync.Once
	result core.Result
	sent   []core.UserInput
	effort string
	// unread accepts live messages without ever reading them, like a run
	// that went idle just as they arrived.
	unread bool
}

func (r *fakeRun) wait() {
	status := <-r.end
	r.result = core.Result{Request: r.req, Status: status}
	r.sink(core.RunFinished{At: time.Now(), Result: r.result})
	close(r.done)
}

func (r *fakeRun) finish(status core.Status) { r.once.Do(func() { r.end <- status }) }

func (r *fakeRun) Send(in core.UserInput) error {
	if !r.caps.LiveInput {
		return engine.ErrUnsupported
	}
	r.sent = append(r.sent, in)
	if !r.unread {
		r.sink(core.UserMessage{At: time.Now(), ID: in.ID, Text: in.Text})
	}

	return nil
}

func (r *fakeRun) SetEffort(e string) error {
	if !r.caps.LiveEffort {
		return engine.ErrUnsupported
	}
	r.effort = e

	return nil
}

func (r *fakeRun) SetModel(string) error       { return engine.ErrUnsupported }
func (r *fakeRun) SetServiceTier(string) error { return engine.ErrUnsupported }
func (r *fakeRun) Interrupt()                  { r.finish(core.StatusInterrupted) }
func (r *fakeRun) Kill()                       { r.finish(core.StatusInterrupted) }

func (r *fakeRun) Wait() (core.Result, error) {
	<-r.done

	return r.result, nil
}

type harness struct {
	t      *testing.T
	eng    *fakeEngine
	s      *session.Session
	events []core.Event
}

func newHarness(t *testing.T, caps engine.Capabilities) *harness {
	t.Helper()
	eng := newFakeEngine(caps)
	s, err := session.Open(context.Background(), eng, session.Options{Settings: settings()})
	require.NoError(t, err)
	h := &harness{t: t, eng: eng, s: s}
	t.Cleanup(func() { _ = s.Close() })
	h.until(isType[session.SessionOpened])

	return h
}

func settings() session.Settings {
	return session.Settings{Provider: "openai-codex", Model: "gpt-6-sol", Effort: "high", Workspace: "/workspace"}
}

// until reads events until one matches, and returns it.
func (h *harness) until(match func(core.Event) bool) core.Event {
	h.t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case e, ok := <-h.s.Events():
			require.True(h.t, ok, "events closed before a match")
			h.events = append(h.events, e)
			if match(e) {
				return e
			}
		case <-timeout:
			h.t.Fatalf("no matching event; saw %d events", len(h.events))
		}
	}
}

func (h *harness) nextRun() *fakeRun {
	h.t.Helper()
	select {
	case r := <-h.eng.started:
		return r
	case <-time.After(5 * time.Second):
		h.t.Fatal("no run started")

		return nil
	}
}

func isType[T core.Event](e core.Event) bool { _, ok := e.(T); return ok }

func typesOf(events []core.Event) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, fmt.Sprintf("%T", e))
	}

	return out
}

func texts(msgs []core.UserInput) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.Text)
	}

	return out
}

func TestSession_SubmitWhenIdleStartsARun(t *testing.T) {
	h := newHarness(t, engine.Capabilities{})

	in, err := h.s.Submit("hello")
	require.NoError(t, err)
	run := h.nextRun()
	h.until(isType[session.InputDelivered])
	run.finish(core.StatusOK)
	h.until(isType[session.Idle])

	assert.Equal(t, []string{"hello"}, texts(run.req.Messages))
	assert.Equal(t, in.ID, run.req.Messages[0].ID)
	assert.Equal(t, h.s.ID(), run.req.SessionID)
	assert.Equal(t, []string{
		"session.SessionOpened", "session.InputQueued", "session.InputSent", "core.RunStarted",
		"core.UserMessage", "session.InputDelivered", "core.RunFinished", "session.Idle",
	}, typesOf(h.events))
}

func TestSession_QueueWhileRunning(t *testing.T) {
	h := newHarness(t, engine.Capabilities{})
	_, err := h.s.Submit("first")
	require.NoError(t, err)
	first := h.nextRun()

	_, err = h.s.Submit("second")
	require.NoError(t, err)
	_, err = h.s.Submit("third")
	require.NoError(t, err)
	first.finish(core.StatusOK)
	next := h.nextRun()

	assert.Equal(t, []string{"second", "third"}, texts(next.req.Messages), "queued messages go out together, in order")
	next.finish(core.StatusOK)
	h.until(isType[session.Idle])
}

func TestSession_InterruptKeepsTheQueue(t *testing.T) {
	h := newHarness(t, engine.Capabilities{})
	_, err := h.s.Submit("work")
	require.NoError(t, err)
	run := h.nextRun()
	_, err = h.s.Submit("later")
	require.NoError(t, err)

	require.NoError(t, h.s.Interrupt())
	finished := h.until(isType[core.RunFinished]).(core.RunFinished)
	h.until(isType[session.Idle])

	assert.Equal(t, core.StatusInterrupted, finished.Result.Status)
	assert.Equal(t, core.StatusInterrupted, run.result.Status)
	select {
	case r := <-h.eng.started:
		t.Fatalf("no run should start after an interrupt, got %v", texts(r.req.Messages))
	case <-time.After(100 * time.Millisecond):
	}

	_, err = h.s.Submit("now")
	require.NoError(t, err)
	next := h.nextRun()
	assert.Equal(t, []string{"later", "now"}, texts(next.req.Messages), "the kept queue goes out before the new message")
	next.finish(core.StatusOK)
}

func TestSession_SteerNow(t *testing.T) {
	t.Run("process engine: interrupt and restart with the queue", func(t *testing.T) {
		h := newHarness(t, engine.Capabilities{})
		_, err := h.s.Submit("work")
		require.NoError(t, err)
		first := h.nextRun()
		_, err = h.s.Submit("queued")
		require.NoError(t, err)

		_, err = h.s.SteerNow("change of plan")
		require.NoError(t, err)
		next := h.nextRun()

		assert.Equal(t, core.StatusInterrupted, first.result.Status)
		assert.Equal(t, []string{"queued", "change of plan"}, texts(next.req.Messages))
		next.finish(core.StatusOK)
	})

	t.Run("live input: the message reaches the running agent", func(t *testing.T) {
		h := newHarness(t, engine.Capabilities{LiveInput: true})
		_, err := h.s.Submit("work")
		require.NoError(t, err)
		run := h.nextRun()
		h.until(isType[session.InputDelivered])

		in, err := h.s.SteerNow("look at the tests too")
		require.NoError(t, err)
		delivered := h.until(isType[session.InputDelivered]).(session.InputDelivered)

		assert.Equal(t, in.ID, delivered.ID)
		assert.Equal(t, []string{"look at the tests too"}, texts(run.sent))
		run.finish(core.StatusOK)
		h.until(isType[session.Idle])
	})
}

func TestSession_Failures(t *testing.T) {
	t.Run("a run that cannot start fails its messages", func(t *testing.T) {
		h := newHarness(t, engine.Capabilities{})
		h.eng.startErr = errors.New("preflight blocked the run: run codex login")

		in, err := h.s.Submit("hello")
		require.NoError(t, err)
		failed := h.until(isType[session.InputFailed]).(session.InputFailed)
		notice := h.until(isType[session.Notice]).(session.Notice)
		h.until(isType[session.Idle])

		assert.Equal(t, []string{in.ID}, failed.IDs)
		assert.Contains(t, failed.Reason, "run codex login")
		assert.Equal(t, "error", notice.Level)
	})

	t.Run("messages the runner never accepted fail when the run ends", func(t *testing.T) {
		h := newHarness(t, engine.Capabilities{})
		h.eng.noEcho = true

		in, err := h.s.Submit("hello")
		require.NoError(t, err)
		h.nextRun().finish(core.StatusFailed)
		failed := h.until(isType[session.InputFailed]).(session.InputFailed)

		assert.Equal(t, []string{in.ID}, failed.IDs)
		assert.Contains(t, failed.Reason, "error")
	})

	t.Run("empty messages are rejected", func(t *testing.T) {
		h := newHarness(t, engine.Capabilities{})

		_, err := h.s.Submit("  ")

		require.Error(t, err)
	})
}

func TestSession_Withdraw(t *testing.T) {
	h := newHarness(t, engine.Capabilities{})
	_, err := h.s.Submit("work")
	require.NoError(t, err)
	run := h.nextRun()
	queued, err := h.s.Submit("never mind")
	require.NoError(t, err)

	ok, err := h.s.Withdraw(queued.ID)
	require.NoError(t, err)
	again, err := h.s.Withdraw(queued.ID)
	require.NoError(t, err)
	run.finish(core.StatusOK)
	h.until(isType[session.Idle])

	assert.True(t, ok)
	assert.False(t, again)
	select {
	case r := <-h.eng.started:
		t.Fatalf("a withdrawn message must not start a run, got %v", texts(r.req.Messages))
	case <-time.After(100 * time.Millisecond):
	}
}

func TestSession_SetSettings(t *testing.T) {
	t.Run("process engine applies changes at the next run", func(t *testing.T) {
		h := newHarness(t, engine.Capabilities{})
		_, err := h.s.Submit("work")
		require.NoError(t, err)
		run := h.nextRun()
		next := settings()
		next.Effort = "low"

		applied, err := h.s.SetSettings(next)
		require.NoError(t, err)
		_, err = h.s.Submit("more")
		require.NoError(t, err)
		run.finish(core.StatusOK)
		second := h.nextRun()

		assert.Equal(t, session.AppliedNextRun, applied)
		assert.Equal(t, "high", run.req.Effort)
		assert.Equal(t, "low", second.req.Effort)
		second.finish(core.StatusOK)
	})

	t.Run("live effort applies now", func(t *testing.T) {
		h := newHarness(t, engine.Capabilities{LiveEffort: true})
		_, err := h.s.Submit("work")
		require.NoError(t, err)
		run := h.nextRun()
		next := settings()
		next.Effort = "max"

		applied, err := h.s.SetSettings(next)
		require.NoError(t, err)

		assert.Equal(t, session.AppliedLive, applied)
		assert.Equal(t, "max", run.effort)
		run.finish(core.StatusOK)
	})

	t.Run("invalid settings are rejected", func(t *testing.T) {
		h := newHarness(t, engine.Capabilities{})
		bad := settings()
		bad.Effort = "huge"

		_, err := h.s.SetSettings(bad)

		require.Error(t, err)
	})
}

func TestSession_CloseInterruptsTheLiveRun(t *testing.T) {
	h := newHarness(t, engine.Capabilities{})
	_, err := h.s.Submit("work")
	require.NoError(t, err)
	run := h.nextRun()

	require.NoError(t, h.s.Close())

	assert.Equal(t, core.StatusInterrupted, run.result.Status)
	for range h.s.Events() { //nolint:revive // drain until closed
	}
	_, err = h.s.Submit("after close")
	require.ErrorIs(t, err, session.ErrClosed)
}

func TestSession_RequeuesLiveMessagesTheRunNeverRead(t *testing.T) {
	h := newHarness(t, engine.Capabilities{LiveInput: true})
	_, err := h.s.Submit("first")
	require.NoError(t, err)
	run := <-h.eng.started
	run.unread = true
	steer, err := h.s.SteerNow("also this")
	require.NoError(t, err)
	h.until(isType[session.InputSent])
	run.finish(core.StatusOK)

	next := <-h.eng.started
	require.Len(t, next.req.Messages, 1)
	assert.Equal(t, steer.ID, next.req.Messages[0].ID, "the next run carries the unread message")
	h.until(func(e core.Event) bool {
		d, ok := e.(session.InputDelivered)

		return ok && d.ID == steer.ID
	})
	next.finish(core.StatusOK)
	h.until(isType[session.Idle])
	for _, e := range h.events {
		assert.NotEqual(t, "session.InputFailed", fmt.Sprintf("%T", e))
	}
}

func TestSession_SteerWhileStartingGoesLive(t *testing.T) {
	h := newHarness(t, engine.Capabilities{LiveInput: true})
	h.eng.gate = make(chan struct{})
	_, err := h.s.Submit("work")
	require.NoError(t, err)
	steer, err := h.s.SteerNow("and this")
	require.NoError(t, err)
	close(h.eng.gate)

	run := h.nextRun()
	h.until(func(e core.Event) bool {
		d, ok := e.(session.InputDelivered)

		return ok && d.ID == steer.ID
	})
	assert.Equal(t, []string{"and this"}, texts(run.sent), "sent live once the run started, not by a restart")
	assert.Equal(t, core.Status(""), run.result.Status, "the starting run was not interrupted")
	run.finish(core.StatusOK)
	h.until(isType[session.Idle])
	assert.Empty(t, h.eng.started, "no second run")
}
