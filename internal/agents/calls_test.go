package agents_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/agents"
	"github.com/viktordanov/uagent-harness/internal/engine"
)

// TestCall_Errors checks the tools' argument errors and unknown agents,
// with Codex's messages.
func TestCall_Errors(t *testing.T) {
	m := agents.New(agents.Config{})
	for _, tc := range []struct{ tool, args, want string }{
		{"spawn_agent", `{"message":"  "}`, "empty message can't be sent to an agent"},
		{"spawn_agent", `{"message":"hi"}`, "subagents are not available in this session"},
		{"send_input", `{"target":"x","message":"hi"}`, "agent with id x not found"},
		{"close_agent", `{"target":"x"}`, "agent with id x not found"},
		{"resume_agent", `{"id":"x"}`, "agent with id x not found"},
		{"wait_agent", `{"targets":[]}`, "agent ids must be non-empty"},
		{"wait_agent", `{"targets":["x"],"timeout_ms":0}`, "timeout_ms must be greater than zero"},
		{"wait_agent", `{"targets":"x"}`, "invalid arguments"},
		{"wait", `{"ids":["x"]}`, `unknown agent tool "wait"`},
	} {
		_, err := m.Call(t.Context(), "p", tc.tool, json.RawMessage(tc.args))
		require.Error(t, err, tc.tool+" "+tc.args)
		assert.Contains(t, err.Error(), tc.want, tc.tool+" "+tc.args)
	}
}

// TestCall_WaitUnknownID reports an unknown agent as not_found at once.
func TestCall_WaitUnknownID(t *testing.T) {
	m := agents.New(agents.Config{})
	out, err := m.Call(t.Context(), "p", "wait_agent", json.RawMessage(`{"targets":["missing"]}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"status":{"missing":"not_found"},"timed_out":false}`, out)
}

// TestTools_Offered offers Codex's v1 tools while the depth allows it, and
// resolves the name uah used before.
func TestTools_Offered(t *testing.T) {
	m := agents.New(agents.Config{MaxDepth: 1, Roles: []agents.Role{{Name: "reviewer", Description: "Reviews diffs."}}})
	var names []string
	for _, tl := range m.Attach(engine.AgentParent{SessionID: "p"}) {
		names = append(names, tl.Name)
		assert.Equal(t, "object", tl.Parameters["type"])
		if tl.Name == "spawn_agent" {
			assert.Contains(t, tl.Description, "- `reviewer`: Reviews diffs.")
		}
	}
	assert.Equal(t, []string{"spawn_agent", "send_input", "wait_agent", "close_agent", "resume_agent"}, names)
	assert.Contains(t, m.ToolNames(), "wait")

	off := agents.New(agents.Config{MaxDepth: 0})
	assert.Empty(t, off.Attach(engine.AgentParent{SessionID: "p"}), "max_depth 0 offers nothing")
}

// TestStatus_JSON encodes statuses as Codex's AgentStatus.
func TestStatus_JSON(t *testing.T) {
	for _, tc := range []struct {
		status agents.Status
		want   string
	}{
		{agents.Status{State: engine.AgentRunning}, `"running"`},
		{agents.Status{State: engine.AgentPendingInit}, `"pending_init"`},
		{agents.Status{State: engine.AgentInterrupted}, `"interrupted"`},
		{agents.Status{State: engine.AgentShutdown}, `"shutdown"`},
		{agents.Status{State: engine.AgentNotFound}, `"not_found"`},
		{agents.Status{State: engine.AgentCompleted, Message: "done"}, `{"completed":"done"}`},
		{agents.Status{State: engine.AgentCompleted}, `{"completed":null}`},
		{agents.Status{State: engine.AgentErrored, Message: "boom"}, `{"errored":"boom"}`},
	} {
		data, err := json.Marshal(tc.status)
		require.NoError(t, err)
		assert.JSONEq(t, tc.want, string(data))
	}
	assert.False(t, agents.Status{State: engine.AgentPendingInit}.Final())
	assert.True(t, agents.Status{State: engine.AgentInterrupted}.Final())
}
