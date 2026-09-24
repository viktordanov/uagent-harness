package state_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/compaction"
	"github.com/viktordanov/uagent-harness/internal/engine"
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
