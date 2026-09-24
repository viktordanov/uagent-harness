package embedded_test

import (
	"context"
	"errors"
	"path/filepath"
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
	require.True(t, s.Capabilities().Compaction)
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
	assert.Equal(t, engine.Compacted{At: done.At, Trigger: compaction.TriggerManual, Summary: "SUMMARY"}, done)

	reqs := e.llm.Requests()
	require.Len(t, reqs, 6)
	summary := reqs[4]
	assert.Equal(t, []string{"first", "second", compaction.Prompt}, summary.UserTexts, "the summary covers the history before the new message")
	assert.Equal(t, []string{"call-1-0", "call-3-0"}, summary.CallIDs)
	assert.Empty(t, summary.Tools)
	assert.Contains(t, summary.System, "You are an AI agent")

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
