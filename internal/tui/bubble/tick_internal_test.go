package bubble

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uah/internal/session"
)

// TestTick_StartsWithTheBatchThatStartsARun: a batch of events that makes
// the session busy starts the clock, even when no other batch follows
// (the model is thinking), so the working line counts the seconds.
func TestTick_StartsWithTheBatchThatStartsARun(t *testing.T) {
	m := New(context.Background(), Deps{Now: time.Now})
	next, _ := m.Update(eventsMsg{gen: m.gen, events: []core.Event{
		session.InputQueued{At: time.Now(), Input: core.UserInput{ID: "m1", Text: "start"}},
	}})

	assert.True(t, next.(Model).st.Busy)
	assert.True(t, next.(Model).ticking, "the tick is scheduled")
}

// TestTick_RunsWhileAReviewRuns: /review's spinner and timer move while the
// reviewer works, with no other event to redraw the screen (issue #3).
func TestTick_RunsWhileAReviewRuns(t *testing.T) {
	m := New(context.Background(), Deps{Now: time.Now})
	next, _ := m.Update(eventsMsg{gen: m.gen, events: []core.Event{
		session.ReviewStarted{At: time.Now(), ID: "r1"},
	}})

	assert.False(t, next.(Model).st.Busy)
	assert.True(t, next.(Model).ticking, "the tick is scheduled")
}
