package state_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

func TestReduce_Agents(t *testing.T) {
	s, _ := apply(opened(), state.Submit{Text: "/agents"})
	assert.Contains(t, s.Items[len(s.Items)-1].Text, "no subagents in this session")

	s, _ = apply(s, engine.AgentUpdated{ID: "a1", Nickname: "Ada", Role: "reviewer", State: engine.AgentRunning})
	n := len(s.Items)
	agent := s.Items[n-1]
	require.Equal(t, state.KindAgent, agent.Kind)
	assert.True(t, agent.Live())

	s, _ = apply(s, engine.AgentUpdated{ID: "a1", Nickname: "Ada", Role: "reviewer", State: engine.AgentCompleted})
	require.Len(t, s.Items, n, "an update changes the line in place")
	assert.Equal(t, engine.AgentCompleted, s.Items[n-1].Detail)
	assert.False(t, s.Items[n-1].Live())

	s, _ = apply(s, state.Submit{Text: "/agents"})
	assert.Equal(t, "Ada (reviewer) · completed · a1", s.Items[len(s.Items)-1].Text)
}
