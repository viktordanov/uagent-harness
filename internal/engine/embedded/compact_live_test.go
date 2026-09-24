package embedded_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/compaction"
	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

func compactions(all []core.Event) (started []engine.CompactionStarted, done []engine.Compacted) {
	for _, x := range all {
		switch v := x.(type) {
		case engine.CompactionStarted:
			started = append(started, v)
		case engine.Compacted:
			done = append(done, v)
		}
	}

	return started, done
}

func TestEmbedded_AutoCompactionCountsOutputAfterTheLastResponse(t *testing.T) {
	e := newEnv(t,
		fakellm.Reply{Commands: []string{"head -c 40000 /dev/zero | tr '\\0' x"}, InputTokens: 100},
		fakellm.Reply{Text: "BIG"},
		fakellm.Reply{Text: "done"},
	)
	s, ev := e.open(t, e.compactingIn(12_000, 80), "")
	ask(t, s, ev, "first")

	reqs := e.llm.Requests()
	require.Len(t, reqs, 3, "the last response used 110 tokens, but the tool output after it fills the window")
	assert.Equal(t, []string{"first", compaction.Prompt}, reqs[1].UserTexts)
	assert.Equal(t, []string{"first", summaryText("BIG")}, reqs[2].UserTexts)
}

func TestEmbedded_AutoCompactionWithoutUsage(t *testing.T) {
	e := newEnv(t,
		fakellm.Reply{Commands: []string{"echo one"}, NoUsage: true},
		fakellm.Reply{Text: "ESTIMATED"},
		fakellm.Reply{Text: "done"},
	)
	s, ev := e.open(t, e.compactingIn(400, 90), "")
	ask(t, s, ev, "first")

	reqs := e.llm.Requests()
	require.Len(t, reqs, 3, "no usage: the whole request is estimated")
	assert.Equal(t, []string{"first", summaryText("ESTIMATED")}, reqs[2].UserTexts)
}

func TestEmbedded_AMessageDuringTheSummaryDoesNotRestartIt(t *testing.T) {
	first, summary := make(chan struct{}), make(chan struct{})
	e := newEnv(t,
		fakellm.Reply{Commands: []string{"echo one"}, Gate: first},
		fakellm.Reply{Text: "KEPT", Gate: summary},
		fakellm.Reply{Text: "done"},
	)
	s, ev := e.open(t, e.compacting(0), "")
	_, err := s.Submit("first")
	require.NoError(t, err)
	waitSeen(t, e.llm, 1)
	require.NoError(t, s.Compact())
	close(first)
	waitSeen(t, e.llm, 2)
	_, err = s.SteerNow("steer")
	require.NoError(t, err)
	ev.until("delivery", isA[session.InputDelivered])
	close(summary)
	ev.finished()
	ev.idle()

	reqs := e.llm.Requests()
	require.Len(t, reqs, 3, "one summary call, not a second one for the new request")
	assert.Equal(t, []string{"first", compaction.Prompt}, reqs[1].UserTexts)
	assert.Equal(t, []string{"first", summaryText("KEPT"), "steer"}, reqs[2].UserTexts)
	started, done := compactions(ev.all)
	assert.Len(t, started, 1)
	require.Len(t, done, 1)
	assert.Empty(t, done[0].Err)
}

func TestEmbedded_InterruptStopsTheSummary(t *testing.T) {
	gate := make(chan struct{})
	t.Cleanup(func() { close(gate) })
	e := newEnv(t,
		fakellm.Reply{Commands: []string{"echo one"}, InputTokens: 250_000},
		fakellm.Reply{Text: "NEVER", Gate: gate},
	)
	s, ev := e.open(t, e.compacting(90), "")
	_, err := s.Submit("first")
	require.NoError(t, err)
	waitSeen(t, e.llm, 2)
	require.NoError(t, s.Interrupt())
	ev.finished()
	ev.idle()

	_, done := compactions(ev.all)
	require.Len(t, done, 1)
	assert.True(t, done[0].Interrupted)
	assert.Len(t, e.llm.Requests(), 2, "no request after the interrupted summary")
	assert.NoFileExists(t, filepath.Join(e.StateDir, "sessions", s.ID()+".compaction.jsonl"))
}

func TestEmbedded_AFailedSummaryStillSendsTheRequest(t *testing.T) {
	e := newEnv(t,
		fakellm.Reply{Text: "answer one"},
		fakellm.Reply{Fail: 400, FailCode: "invalid_prompt"},
		fakellm.Reply{Text: "answer two"},
	)
	s, ev := e.open(t, e.compacting(0), "")
	ask(t, s, ev, "first")
	require.NoError(t, s.Compact())
	ask(t, s, ev, "second")

	reqs := e.llm.Requests()
	require.Len(t, reqs, 3)
	assert.Equal(t, []string{"first", "second"}, reqs[2].UserTexts, "the request goes out uncompacted")
	_, done := compactions(ev.all)
	require.Len(t, done, 1)
	assert.Contains(t, done[0].Err, "invalid_prompt")
	assert.False(t, done[0].Interrupted)
}

func TestEmbedded_AutoCompactionStopsAfterRepeatedFailures(t *testing.T) {
	var replies []fakellm.Reply
	for range 3 {
		replies = append(replies,
			fakellm.Reply{Commands: []string{"echo more"}, InputTokens: 250_000},
			fakellm.Reply{Fail: 400, FailCode: "invalid_prompt"})
	}
	replies = append(replies, fakellm.Reply{Commands: []string{"echo last"}, InputTokens: 250_000}, fakellm.Reply{Text: "done"})
	e := newEnv(t, replies...)
	s, ev := e.open(t, e.compacting(90), "")
	ask(t, s, ev, "first")

	assert.Len(t, e.llm.Requests(), 8, "three failed summaries, then none")
	started, _ := compactions(ev.all)
	assert.Len(t, started, 3)
}

func TestEmbedded_ResumeSurvivesABadCompactionLog(t *testing.T) {
	e := newEnv(t, fakellm.Reply{Text: "answer one"}, fakellm.Reply{Text: "answer two"})
	s, ev := e.open(t, e.compacting(0), "")
	ask(t, s, ev, "first")
	id := s.ID()
	require.NoError(t, s.Close())

	// A record for another history, and a line cut short by a crash.
	path := filepath.Join(e.StateDir, "sessions", id+".compaction.jsonl")
	require.NoError(t, compaction.OpenLog(filepath.Dir(path), id).Append(compaction.Record{Covered: 1, Hash: "other", Summary: "stale"}))
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString(`{"covered":2,"ha`)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	s2, ev2 := e.open(t, e.compacting(0), id)
	ask(t, s2, ev2, "second")
	reqs := e.llm.Requests()
	require.Len(t, reqs, 2)
	assert.Equal(t, []string{"first", "second"}, reqs[1].UserTexts, "a record that does not match sends the full history")
	_, done := compactions(ev2.all)
	require.Len(t, done, 1, "the mismatch is reported once")
	assert.Contains(t, done[0].Err, "no longer matches")
}
