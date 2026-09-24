package agents_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/agents"
	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

const unsupported = "The 'gpt-luna-6' model is not supported when using Codex with a ChatGPT account."

// TestAgents_FailureReachesTheParent carries the provider's message of a
// child whose model request failed to the parent: in wait_agent's errored
// status and in the parent's AgentUpdated.
func TestAgents_FailureReachesTheParent(t *testing.T) {
	e := newEnv(t, agents.Config{},
		fakellm.Reply{Calls: []fakellm.Call{call("spawn_agent", `{"message":"CHILD-BAD work","model":"gpt-luna-6"}`)}},
		callWith("wait_agent", `{"targets":["ID"]}`),
		fakellm.Reply{Text: "told the user"},
	)
	e.llm.Route("CHILD-BAD", fakellm.Reply{Fail: 400, FailBody: `{"detail":"` + unsupported + `"}`})
	s, ev := e.open(t, false)

	_, err := s.Submit("delegate")
	require.NoError(t, err)
	assert.Equal(t, "told the user", ev.finished().Answer)

	assert.Contains(t, lastOutputs(e), `{"errored":"`+unsupported+`"}`, "wait_agent says why the child failed")
	failed := ev.agentState(engine.AgentErrored)
	assert.Equal(t, unsupported, failed.Message, "so does the parent's stream, for the TUI and uah run")
	assert.Equal(t, "gpt-luna-6", failed.Model)
	assert.Equal(t, "CHILD-BAD work", failed.Task)
	assert.NotEmpty(t, failed.CallID)
}
