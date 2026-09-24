package state_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

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

func TestReduce_AgentActivity(t *testing.T) {
	s, _ := apply(opened(), engine.AgentUpdated{ID: "a1", Nickname: "Ada", State: engine.AgentRunning})
	s, _ = apply(s, engine.AgentActivity{ID: "a1", Event: core.ToolCalled{CallID: "c1", Name: "Bash", Label: "ls"}})
	s, _ = apply(s, engine.AgentActivity{ID: "a1", Event: core.ToolStarted{CallID: "c1"}})
	s, _ = apply(s, engine.AgentActivity{ID: "a2", Event: core.ToolCalled{CallID: "c9", Name: "Bash"}}) // unknown agent: ignored
	agent := s.Items[len(s.Items)-1]
	require.Len(t, agent.Sub, 1)
	assert.Equal(t, state.ToolRunning, agent.Sub[0].Tool)
	assert.Equal(t, "ls", agent.Sub[0].Label)

	s, _ = apply(s, engine.AgentUpdated{ID: "a1", Nickname: "Ada", State: engine.AgentInterrupted})
	assert.Equal(t, state.ToolStopped, s.Items[len(s.Items)-1].Sub[0].Tool, "a stopped agent's running tool shows as stopped")

	for i := range 40 {
		s, _ = apply(s, engine.AgentActivity{ID: "a1", Event: core.ToolCalled{CallID: fmt.Sprint(i), Name: "Bash"}})
	}
	assert.Len(t, s.Items[len(s.Items)-1].Sub, 30, "the line keeps the latest tool calls")
}
