package state_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent/core"
	uaharness "github.com/viktordanov/uagent/harness"

	"github.com/viktordanov/uagent-harness/internal/compaction"
	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

func TestReduce_ContextMeter(t *testing.T) {
	s := opened()
	_, ok := s.ContextLeft()
	assert.False(t, ok, "unknown before the first response")

	s, _ = apply(s, core.ModelResponded{At: t0, Turn: 1, Usage: core.Tokens{InputTokens: 136_000, OutputTokens: 6_000}})
	left, ok := s.ContextLeft()
	assert.True(t, ok)
	assert.Equal(t, 50, left)

	s.Settings.ContextWindow = 1_000_000
	left, _ = s.ContextLeft()
	assert.Equal(t, 87, left, "the configured window wins over the model table")

	s, _ = apply(s, engine.Compacted{At: t0, Trigger: compaction.TriggerManual, Summary: "s"})
	_, ok = s.ContextLeft()
	assert.False(t, ok, "a compaction clears the meter until the next response")
}

func TestReduce_CompactCommand(t *testing.T) {
	s, effects := apply(opened(), state.Submit{Text: "/compact"})
	assert.Empty(t, effects, "the process engine cannot compact")
	assert.Contains(t, s.Items[len(s.Items)-1].Text, "needs the embedded engine")

	s = opened()
	s.Caps = engine.Capabilities{Compaction: true}
	_, effects = apply(s, state.Submit{Text: "/compact"})
	assert.Equal(t, []state.Effect{state.EffCompact{}}, effects)
}

func TestReduce_ReloadedCompactionAndInterrupt(t *testing.T) {
	s := opened()
	history := []session.LoadedRun{{
		Record: uaharness.RunRecord{Complete: true, Result: core.Result{
			Request: core.Request{RunID: "old", SessionID: "sess-2"}, Status: core.StatusOK, StartedAt: t0, Wall: time.Second,
		}},
		Events: []core.Event{
			core.ModelResponded{At: t0, Turn: 1, Usage: core.Tokens{InputTokens: 200_000}},
			engine.Compacted{At: t0, Trigger: compaction.TriggerAuto, Summary: "S"},
		},
	}}
	s, _ = apply(s, state.HistoryLoaded{SessionID: "sess-2", Runs: history})
	var notices []string
	for _, it := range s.Items {
		if it.Kind == state.KindNotice {
			notices = append(notices, it.Text)
		}
	}
	assert.Contains(t, notices, "Context compacted; your messages stay as written", "a reloaded transcript shows the compaction")
	_, ok := s.ContextLeft()
	assert.False(t, ok, "the reloaded compaction clears the meter")

	s, _ = apply(s, engine.Compacted{At: t0, Trigger: compaction.TriggerManual, Err: "interrupted: context canceled", Interrupted: true})
	assert.Equal(t, "Compaction interrupted", s.Items[len(s.Items)-1].Text)
}

func TestReduce_ClearStaysInTheSession(t *testing.T) {
	s := opened()
	s.Caps = engine.Capabilities{Compaction: true}
	s, _ = apply(s, state.Submit{Text: "hello"})
	id := s.SessionID
	s, effects := apply(s, state.Submit{Text: "/clear"})
	assert.Equal(t, []state.Effect{state.EffClear{}}, effects, "no new session")
	assert.Equal(t, id, s.SessionID)
	assert.Len(t, s.Items, 1, "the screen clears to one notice")
	assert.Contains(t, s.Items[0].Text, "Context cleared")

	s, _ = apply(s, engine.CompactionStarted{At: t0, Trigger: compaction.TriggerClear}, engine.Compacted{At: t0, Trigger: compaction.TriggerClear})
	assert.Len(t, s.Items, 2, "the engine's report is a debug line only")
	assert.Equal(t, state.LevelDebug, s.Items[1].Level)
}
