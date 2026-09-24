package session_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/session"
)

func newInteractive(t *testing.T) *harness {
	t.Helper()
	eng := newFakeEngine(engine.Capabilities{})
	s, err := session.Open(context.Background(), eng, session.Options{Settings: settings(), Interactive: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	h := &harness{t: t, eng: eng, s: s}
	h.until(isType[session.SessionOpened])

	return h
}

// TestSession_AskAnytimeOutlivesTheRun keeps an AskAnytime prompt open when
// the run ends and asks one while the session is idle, while the run's own
// prompts end with it.
func TestSession_AskAnytimeOutlivesTheRun(t *testing.T) {
	h := newInteractive(t)
	_, err := h.s.Submit("hello")
	require.NoError(t, err)
	run := h.nextRun()
	require.NotNil(t, run.opts.AskAnytime)

	during := make(chan approval.Answer, 1)
	go func() { during <- run.opts.AskAnytime(context.Background(), approval.Prompt{Command: "during"}) }()
	first := h.until(isType[session.ApprovalRequested]).(session.ApprovalRequested)
	run.finish(core.StatusOK)
	h.until(isType[session.Idle])

	after := make(chan approval.Answer, 1)
	go func() { after <- run.opts.AskAnytime(context.Background(), approval.Prompt{Command: "after"}) }()
	second := h.until(isType[session.ApprovalRequested]).(session.ApprovalRequested)
	assert.Equal(t, "after", second.Command)
	require.NoError(t, h.s.Resolve(first.ID, approval.Approve))
	require.NoError(t, h.s.Resolve(second.ID, approval.Decline))
	assert.Equal(t, approval.Approve, <-during, "the prompt stayed open after the run ended")
	assert.Equal(t, approval.Decline, <-after)

	assert.Equal(t, approval.Decline, run.opts.Ask(context.Background(), approval.Prompt{Command: "late"}), "a run's own prompt is declined once it ended")
}

// TestSession_AskAnytimeEndsWithItsContext declines an open prompt whose
// context ends.
func TestSession_AskAnytimeEndsWithItsContext(t *testing.T) {
	h := newInteractive(t)
	_, err := h.s.Submit("hello")
	require.NoError(t, err)
	run := h.nextRun()
	run.finish(core.StatusOK)
	h.until(isType[session.Idle])

	ctx, cancel := context.WithCancel(context.Background())
	answer := make(chan approval.Answer, 1)
	go func() { answer <- run.opts.AskAnytime(ctx, approval.Prompt{Command: "x"}) }()
	req := h.until(isType[session.ApprovalRequested]).(session.ApprovalRequested)
	cancel()
	resolved := h.until(isType[session.ApprovalResolved]).(session.ApprovalResolved)

	assert.Equal(t, req.ID, resolved.ID)
	assert.Equal(t, approval.Decline, <-answer)
}
