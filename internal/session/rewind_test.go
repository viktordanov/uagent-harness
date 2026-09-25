package session_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/session"
)

// rewindingEngine is the fake engine with a Rewind that records its calls
// and hands back held texts.
type rewindingEngine struct {
	*fakeEngine
	rewinds []string
	held    []string
}

func (e *rewindingEngine) Rewind(_ context.Context, _, messageID string) (engine.Rewound, []string, error) {
	e.rewinds = append(e.rewinds, messageID)

	return engine.Rewound{At: time.Now(), MessageID: messageID}, e.held, nil
}

func TestRewind_NeedsTheEngine(t *testing.T) {
	h := newHarness(t, engine.Capabilities{})
	require.ErrorIs(t, h.s.Rewind("m1"), session.ErrNoRewind)
}

func TestRewind_WaitsForAnIdleSessionAndSendsHeldTextsAgain(t *testing.T) {
	eng := &rewindingEngine{fakeEngine: newFakeEngine(engine.Capabilities{Rewind: true}), held: []string{"a note"}}
	s, err := session.Open(context.Background(), eng, session.Options{Settings: settings()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	h := &harness{t: t, eng: eng.fakeEngine, s: s}

	in, err := s.Submit("first")
	require.NoError(t, err)
	run := h.nextRun()
	require.ErrorIs(t, s.Rewind(in.ID), session.ErrRewindBusy, "not while the agent works")
	run.finish(core.StatusOK)
	h.until(isType[session.Idle])

	require.NoError(t, s.Rewind(in.ID))
	ev := h.until(isType[engine.Rewound]).(engine.Rewound)
	assert.Equal(t, in.ID, ev.MessageID)
	assert.Equal(t, []string{in.ID}, eng.rewinds)

	_, err = s.Submit("first, edited")
	require.NoError(t, err)
	next := h.nextRun()
	require.Len(t, next.req.Messages, 2)
	assert.Equal(t, "a note", next.req.Messages[0].Text, "what went with the message goes again first")
	assert.NotEqual(t, in.ID, next.req.Messages[0].ID)
	assert.Equal(t, "first, edited", next.req.Messages[1].Text)
}
