package embedded_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/compaction"
	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/engine/embedded"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

func (e *env) compacting(percent int) *embedded.Engine {
	return embedded.New(embedded.Config{StateDir: e.StateDir, Provider: "openai", Getenv: e.getenv, Compaction: compaction.Settings{Percent: percent}})
}

// compactingIn compacts automatically at percent of a window of this many tokens.
func (e *env) compactingIn(window int64, percent int) *embedded.Engine {
	return embedded.New(embedded.Config{StateDir: e.StateDir, Provider: "openai", Getenv: e.getenv, Compaction: compaction.Settings{Percent: percent}, ContextWindow: window})
}

func summaryText(s string) string { return compaction.SummaryPrefix + "\n" + s }

// ask sends a message and waits for the session to be idle again.
func ask(t *testing.T, s interface {
	Submit(string) (core.UserInput, error)
}, ev *events, text string,
) {
	t.Helper()
	_, err := s.Submit(text)
	require.NoError(t, err)
	ev.finished()
	ev.idle()
}

func TestEmbedded_CompactKeepsUserMessagesAndResumes(t *testing.T) {
	e := newEnv(t,
		fakellm.Reply{Commands: []string{"echo one"}},
		fakellm.Reply{Text: "answer one"},
		fakellm.Reply{Commands: []string{"echo two"}},
		fakellm.Reply{Text: "answer two"},
		fakellm.Reply{Text: "SUMMARY"},
		fakellm.Reply{Text: "answer three"},
		fakellm.Reply{Text: "answer four"},
	)
	s, ev := e.open(t, e.compacting(0), "")
	ask(t, s, ev, "first")
	ask(t, s, ev, "second")
	require.NoError(t, s.Compact())
	ask(t, s, ev, "third")

	var sawStart bool
	var done engine.Compacted
	for _, x := range ev.all {
		switch v := x.(type) {
		case engine.CompactionStarted:
			sawStart = v.Trigger == compaction.TriggerManual
		case engine.Compacted:
			done = v
		}
	}
	assert.True(t, sawStart)
	require.NotNil(t, done.Stats, "every compaction is measured")
	stats := *done.Stats
	assert.Equal(t, engine.Compacted{At: done.At, Trigger: compaction.TriggerManual, Summary: "SUMMARY", Stats: done.Stats}, done)
	assert.Equal(t, compaction.StrategyLocal, stats.Strategy)
	assert.Equal(t, compaction.PhasePreTurn, stats.Phase, "compacted before the new message's first request")
	assert.Equal(t, int64(2), stats.SummaryTokens)
	assert.Positive(t, stats.Call.Input, "the summary call's usage")
	assert.Positive(t, stats.Before)
	assert.Positive(t, stats.After)
	records, _, err := compaction.OpenLog(filepath.Join(e.StateDir, "sessions"), s.ID()).Records()
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, done.Stats, records[0].Stats, "the record keeps the stats")

	reqs := e.llm.Requests()
	require.Len(t, reqs, 6)
	summary := reqs[4]
	assert.Equal(t, []string{"first", "second", compaction.Prompt}, summary.UserTexts, "the summary covers the history before the new message")
	assert.Equal(t, []string{"call-1-0", "call-3-0"}, summary.CallIDs)
	assert.Empty(t, summary.Tools)
	assert.Contains(t, summary.System, "You are uah")

	next := reqs[5]
	assert.Equal(t, []string{"first", "second", summaryText("SUMMARY"), "third"}, next.UserTexts)
	assert.Empty(t, next.CallIDs, "older tool calls are replaced by the summary")
	assert.Empty(t, next.ToolOutputs)
	assert.NotEmpty(t, next.Tools)
	assert.FileExists(t, filepath.Join(e.StateDir, "sessions", s.ID()+".compaction.jsonl"))

	id := s.ID()
	require.NoError(t, s.Close())
	s2, ev2 := e.open(t, e.compacting(0), id)
	ask(t, s2, ev2, "fourth")
	reqs = e.llm.Requests()
	require.Len(t, reqs, 7)
	assert.Equal(t, []string{"first", "second", summaryText("SUMMARY"), "third", "fourth"}, reqs[6].UserTexts, "a resumed session keeps its compaction")
	assert.Empty(t, reqs[6].CallIDs)
}

func TestEmbedded_AutoCompactsAtTheThreshold(t *testing.T) {
	e := newEnv(t,
		fakellm.Reply{Commands: []string{"echo one"}, InputTokens: 250_000},
		fakellm.Reply{Text: "AUTO"},
		fakellm.Reply{Text: "done"},
	)
	s, ev := e.open(t, e.compacting(90), "")
	ask(t, s, ev, "first")

	reqs := e.llm.Requests()
	require.Len(t, reqs, 3)
	assert.Equal(t, []string{"first", compaction.Prompt}, reqs[1].UserTexts)
	assert.Equal(t, []string{"call-1-0"}, reqs[1].CallIDs)
	assert.Equal(t, []string{"first", summaryText("AUTO")}, reqs[2].UserTexts)
	assert.Empty(t, reqs[2].CallIDs)
	assert.Empty(t, reqs[2].ToolOutputs, "the tool output is in the summary")
	assert.Equal(t, 1, countKind[engine.CompactionStarted](ev.all))
	for _, x := range ev.all {
		if v, ok := x.(engine.Compacted); ok {
			require.NotNil(t, v.Stats)
			assert.Equal(t, compaction.PhaseMidTurn, v.Stats.Phase, "compacted after the tool output")
			assert.Greater(t, v.Stats.Before, v.Stats.After)
		}
		if v, ok := x.(engine.CompactionStarted); ok {
			assert.Equal(t, compaction.TriggerAuto, v.Trigger)
			assert.Greater(t, v.Tokens, int64(250_010), "the last response plus the tool output after it")
		}
	}
}

func TestEmbedded_NoAutoCompactionBelowTheThreshold(t *testing.T) {
	e := newEnv(t, fakellm.Reply{Commands: []string{"echo one"}, InputTokens: 200_000}, fakellm.Reply{Text: "done"})
	s, ev := e.open(t, e.compacting(90), "")
	ask(t, s, ev, "first")
	require.Len(t, e.llm.Requests(), 2)
	assert.Equal(t, 0, countKind[engine.CompactionStarted](ev.all))
}

func TestEmbedded_CompactsALiveRun(t *testing.T) {
	gate := make(chan struct{})
	e := newEnv(t,
		fakellm.Reply{Commands: []string{"echo one"}, Gate: gate},
		fakellm.Reply{Text: "LIVE"},
		fakellm.Reply{Text: "done"},
	)
	s, ev := e.open(t, e.compacting(0), "")
	_, err := s.Submit("first")
	require.NoError(t, err)
	waitSeen(t, e.llm, 1)
	require.NoError(t, s.Compact())
	close(gate)
	ev.finished()
	ev.idle()

	reqs := e.llm.Requests()
	require.Len(t, reqs, 3)
	assert.Equal(t, []string{"first", compaction.Prompt}, reqs[1].UserTexts)
	assert.Equal(t, []string{"first", summaryText("LIVE")}, reqs[2].UserTexts)
}

func TestEmbedded_BeforeCompactCanStopIt(t *testing.T) {
	e := newEnv(t, fakellm.Reply{Text: "answer one"}, fakellm.Reply{Text: "answer two"})
	var gotSession string
	var gotTrigger compaction.Trigger
	eng := embedded.New(embedded.Config{
		StateDir: e.StateDir, Provider: "openai", Getenv: e.getenv,
		BeforeCompact: func(_ context.Context, sessionID string, t compaction.Trigger) error {
			gotSession, gotTrigger = sessionID, t

			return errors.New("not now")
		},
	})
	s, ev := e.open(t, eng, "")
	ask(t, s, ev, "first")
	require.NoError(t, s.Compact())
	ask(t, s, ev, "second")

	assert.Equal(t, s.ID(), gotSession, "the hook gets the session")
	assert.Equal(t, compaction.TriggerManual, gotTrigger)
	reqs := e.llm.Requests()
	require.Len(t, reqs, 2, "no summary request: the compaction was stopped")
	assert.Equal(t, []string{"first", "second"}, reqs[1].UserTexts)
}

func TestEmbedded_TheSummaryCarriesTheStateLedger(t *testing.T) {
	e := newEnv(t,
		fakellm.Reply{Commands: []string{"cat notes.md; echo boom >&2; exit 3"}},
		fakellm.Reply{Text: "answer one"},
		fakellm.Reply{Text: "SUMMARY"},
		fakellm.Reply{Text: "answer two"},
	)
	s, ev := e.open(t, e.compacting(0), "")
	ask(t, s, ev, "first")
	require.NoError(t, s.Compact())
	ask(t, s, ev, "second")

	reqs := e.llm.Requests()
	require.Len(t, reqs, 4)
	summary := reqs[3].UserTexts[1]
	assert.True(t, strings.HasPrefix(summary, summaryText("SUMMARY")+"\n\n<uah_state_ledger>"), summary)
	assert.Contains(t, summary, "Failing commands (their last run failed): `cat notes.md; echo boom >&2; exit 3` exit 3: ")
	assert.Contains(t, summary, "boom")
	assert.Contains(t, summary, "Files read: notes.md")
	_, done := compactions(ev.all)
	require.Len(t, done, 1)
	assert.Positive(t, done[0].Stats.LedgerTokens)
}

func TestEmbedded_AutoCompactionElidesOldOutputsFirst(t *testing.T) {
	big := "head -c 8000 /dev/zero | tr '\\0' x"
	e := newEnv(t,
		fakellm.Reply{Commands: []string{big}},
		fakellm.Reply{Commands: []string{big}},
		fakellm.Reply{Commands: []string{big}},
		fakellm.Reply{Commands: []string{big}, InputTokens: 17_000},
		fakellm.Reply{Text: "done"},
		fakellm.Reply{Text: "again"},
	)
	settings := compaction.Settings{Percent: 90, Elision: compaction.Elision{AfterCalls: 2}}
	eng := embedded.New(embedded.Config{StateDir: e.StateDir, Provider: "openai", Getenv: e.getenv, Compaction: settings, ContextWindow: 20_000})
	s, ev := e.open(t, eng, "")
	ask(t, s, ev, "first")

	reqs := e.llm.Requests()
	require.Len(t, reqs, 5, "the stubs freed enough: no summary call")
	outs := reqs[4].ToolOutputs
	require.Len(t, outs, 4)
	for i, want := range []bool{true, true, false, false} {
		assert.Equal(t, want, strings.HasPrefix(outs[i], "[uah elided this output to save context: Bash "), "output %d", i)
	}
	assert.Contains(t, outs[0], "2,00", "the stub gives the size")
	_, done := compactions(ev.all)
	require.Len(t, done, 1)
	require.NotNil(t, done[0].Stats)
	assert.Equal(t, compaction.StrategyElide, done[0].Stats.Strategy)
	assert.Equal(t, 2, done[0].Stats.Elided)
	assert.Greater(t, done[0].Stats.Before, done[0].Stats.After)

	// Sticky: the next request, and a resumed session, send the same stubs.
	ask(t, s, ev, "second")
	id := s.ID()
	require.NoError(t, s.Close())
	s2, ev2 := e.open(t, eng, id)
	ask(t, s2, ev2, "third")
	reqs = e.llm.Requests()
	require.Len(t, reqs, 7)
	assert.Equal(t, outs, reqs[5].ToolOutputs[:4])
	assert.Equal(t, outs, reqs[6].ToolOutputs[:4])
}
