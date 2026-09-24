package agents_test

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/agents"
	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

// TestAgents_Watch follows a child from the parent session, as the TUI's
// agent view does: what it did so far, what it does next, and a message
// from the user that the child answers.
func TestAgents_Watch(t *testing.T) {
	gate := make(chan struct{})
	e := newEnv(t, agents.Config{},
		fakellm.Reply{Calls: []fakellm.Call{call("spawn_agent", `{"message":"CHILD-W look"}`)}},
		fakellm.Reply{Text: "spawned"},
	)
	e.llm.Route("CHILD-W", fakellm.Reply{Gate: gate, Text: "first answer"}, fakellm.Reply{Text: "second answer"})
	s, ev := e.open(t, false)
	_, err := s.Submit("start one")
	require.NoError(t, err)
	ev.finished()
	ev.agentState(engine.AgentRunning)

	_, err = s.WatchAgent("nobody")
	require.Error(t, err)
	w, err := s.WatchAgent("ada")
	require.NoError(t, err, "by nickname, in any case")
	defer w.Stop()
	assert.Equal(t, "Ada", w.Nickname)
	require.NotEmpty(t, w.Events)
	opened, ok := w.Events[0].(session.SessionOpened)
	require.True(t, ok, "the view starts where the child's session started")
	assert.Equal(t, w.ID, opened.ID)

	close(gate)
	answers := func(want string) {
		t.Helper()
		deadline := time.After(waitTimeout)
		for {
			select {
			case x, ok := <-w.Next:
				require.True(t, ok, "the watch ended before %q", want)
				if m, ok := x.(core.AssistantMessage); ok && m.Text == want {
					return
				}
			case <-deadline:
				t.Fatalf("no %q", want)
			}
		}
	}
	answers("first answer")
	require.NoError(t, w.Send("CHILD-W and now?", false))
	answers("second answer")
	assert.True(t, slices.ContainsFunc(e.llm.Requests(), func(r fakellm.Request) bool {
		return slices.Contains(r.UserTexts, "CHILD-W and now?")
	}), "the user's message reached the child")
	assert.Equal(t, engine.AgentCompleted, ev.agentState(engine.AgentCompleted).State)
}
