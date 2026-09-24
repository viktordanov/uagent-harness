package state_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/session"
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
	assert.Equal(t, "Ada (reviewer) · completed · a1\n/agents <name> shows one's transcript as it works; alt+← and alt+→ switch agents", s.Items[len(s.Items)-1].Text)

	s, _ = apply(s, engine.AgentUpdated{ID: "a1", Nickname: "Ada", State: engine.AgentErrored, Message: "The model is not supported"})
	assert.Equal(t, "The model is not supported", s.Items[n-1].Agent.Message, "the item keeps the latest update")
	s, _ = apply(s, state.Submit{Text: "/agents"})
	assert.Contains(t, s.Items[len(s.Items)-1].Text, "Ada (reviewer) · errored: The model is not supported · a1")
}

// TestReduce_AgentView opens a subagent's transcript by nickname: the
// session keeps reducing its own events, messages go to the agent, other
// commands wait, and esc returns.
func TestReduce_AgentView(t *testing.T) {
	s, _ := apply(opened(), engine.AgentUpdated{ID: "subagent-1", Nickname: "Ada", State: engine.AgentRunning})
	s, eff := apply(s, state.Submit{Text: "/agents ada"})
	require.Equal(t, []state.Effect{state.EffViewAgent{ID: "subagent-1"}}, eff)
	assert.Contains(t, s.Suggestions("/agents A")[0].Label, "Ada", "/agents completes the nicknames")

	child := []core.Event{
		session.SessionOpened{ID: "subagent-1", Settings: session.Settings{Model: "gpt-child"}},
		session.InputQueued{Input: core.UserInput{ID: "m1", Text: "count the files"}},
		session.InputSent{IDs: []string{"m1"}},
	}
	s, _ = apply(s, state.AgentViewOpened{ID: "subagent-1", Nickname: "Ada", Events: child})
	require.NotNil(t, s.View)
	assert.Equal(t, "gpt-child", s.View.St.Settings.Model)
	assert.Equal(t, "count the files", s.View.St.Items[len(s.View.St.Items)-1].Text)

	parentItems := len(s.Items)
	s, _ = apply(s, core.AssistantMessage{Text: "the parent works on", Final: true})
	assert.Len(t, s.Items, parentItems+1, "the session's events still reach its own transcript")
	s, _ = apply(s, state.AgentEvents{ID: "subagent-1", Events: []core.Event{core.AssistantMessage{Text: "seven files", Final: true}}})
	assert.Equal(t, "seven files", s.View.St.Items[len(s.View.St.Items)-1].Text)

	s, eff = apply(s, state.Submit{Text: "and the dirs?"})
	assert.Equal(t, []state.Effect{state.EffAgentSend{ID: "subagent-1", Text: "and the dirs?"}}, eff)
	s, eff = apply(s, state.Submit{Text: "/model x"})
	assert.Empty(t, eff)
	assert.Contains(t, s.View.St.Items[len(s.View.St.Items)-1].Text, "/model is for the main agent")

	// esc esc interrupts the agent while it works, as for the main agent.
	s, _ = apply(s, state.AgentEvents{ID: "subagent-1", Events: []core.Event{core.RunStarted{At: t0, RunID: "r1", SessionID: "subagent-1"}}})
	s, eff = apply(s, state.Esc{})
	assert.Empty(t, eff)
	require.NotNil(t, s.View, "esc does not leave the view")
	assert.Contains(t, s.View.St.Status, "press esc again to interrupt Ada")
	s, eff = apply(s, state.Esc{})
	assert.Equal(t, []state.Effect{state.EffAgentInterrupt{ID: "subagent-1"}}, eff)

	// alt+← and alt+→ cycle the main agent and the subagents, wrapping.
	s, eff = apply(s, state.SwitchAgent{Delta: 1})
	assert.Nil(t, s.View, "after the last agent comes the main agent")
	assert.Equal(t, []state.Effect{state.EffCloseAgentView{}}, eff)
	_, eff = apply(s, state.SwitchAgent{Delta: -1})
	assert.Equal(t, []state.Effect{state.EffViewAgent{ID: "subagent-1"}}, eff)
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

func TestReduce_AgentCallsReadAsNames(t *testing.T) {
	s := opened()
	s, _ = apply(s,
		core.ToolCalled{At: t0, CallID: "c1", Name: "spawn_agent", Label: `{"message":"Summarize"}`},
		engine.AgentUpdated{At: t0, ID: "subagent-1234abcd-0000", Nickname: "Ada", State: engine.AgentRunning, Started: t0, CallID: "c1", Model: "gpt-6-luna", Effort: "low", Task: "Summarize"},
		core.ToolCalled{At: t0, CallID: "c2", Name: "wait_agent", Label: `{"targets":["subagent-1234abcd-0000","subagent-99999999-0000"],"timeout_ms":1000}`},
		core.ToolCalled{At: t0, CallID: "c3", Name: "send_input", Label: `{"target":"subagent-1234abcd-0000","message":"and the tests"}`},
		engine.AgentUpdated{At: t0.Add(72 * time.Second), ID: "subagent-1234abcd-0000", Nickname: "Ada", State: engine.AgentCompleted, Started: t0},
		engine.AgentUpdated{At: t0.Add(80 * time.Second), ID: "subagent-1234abcd-0000", Nickname: "Ada", State: engine.AgentShutdown, Started: t0},
	)
	label := func(key string) string {
		for _, it := range s.Items {
			if it.Key == key {
				return it.Label
			}
		}

		return ""
	}
	assert.Equal(t, "Ada · gpt-6-luna low · Summarize", label("call:c1"))
	assert.Equal(t, "Ada, subagent-99999999", label("call:c2"))
	assert.Equal(t, "Ada · and the tests", label("call:c3"))
	for _, it := range s.Items {
		if it.Kind == state.KindAgent {
			assert.Equal(t, engine.AgentCompleted, it.Detail, "closing a finished agent keeps done")
			assert.Equal(t, 72*time.Second, it.Duration)
		}
	}
}
