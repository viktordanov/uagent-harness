package embedded_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/compaction"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

func TestEmbedded_ClearStartsFreshInTheSameSession(t *testing.T) {
	e := newEnv(t,
		fakellm.Reply{Commands: []string{"echo one"}},
		fakellm.Reply{Text: "answer one"},
		fakellm.Reply{Text: "answer two"},
		fakellm.Reply{Text: "SUMMARY"},
		fakellm.Reply{Text: "answer three"},
		fakellm.Reply{Text: "answer four"},
	)
	s, ev := e.open(t, e.compacting(0), "")
	ask(t, s, ev, "first")
	require.NoError(t, s.Clear())
	ask(t, s, ev, "second")

	reqs := e.llm.Requests()
	require.Len(t, reqs, 3, "a clear makes no model call")
	assert.Equal(t, []string{"second"}, reqs[2].UserTexts, "nothing from before the clear")
	assert.Empty(t, reqs[2].CallIDs)
	assert.NotEmpty(t, reqs[2].Tools)
	started, done := compactions(ev.all)
	require.Len(t, started, 1)
	assert.Equal(t, compaction.TriggerClear, started[0].Trigger)
	require.Len(t, done, 1)
	assert.Empty(t, done[0].Err)

	// A later compaction keeps only the messages after the clear.
	require.NoError(t, s.Compact())
	ask(t, s, ev, "third")
	reqs = e.llm.Requests()
	assert.Equal(t, []string{"second", compaction.Prompt}, reqs[3].UserTexts)
	assert.Equal(t, []string{"second", summaryText("SUMMARY"), "third"}, reqs[4].UserTexts)

	// The session is the same one, and resuming it keeps the clear.
	id := s.ID()
	require.NoError(t, s.Close())
	s2, ev2 := e.open(t, e.compacting(0), id)
	ask(t, s2, ev2, "fourth")
	reqs = e.llm.Requests()
	assert.Equal(t, []string{"second", summaryText("SUMMARY"), "third", "fourth"}, reqs[len(reqs)-1].UserTexts)
}
