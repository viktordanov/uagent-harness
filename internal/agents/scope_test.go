package agents_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/agents"
	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/engine/embedded"
	"github.com/viktordanov/uagent-harness/internal/rules"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

// waitAll is a reply that waits for every agent spawned so far.
func waitAll() fakellm.Reply {
	return fakellm.Reply{From: func(req fakellm.Request) fakellm.Reply {
		return fakellm.Reply{Calls: []fakellm.Call{call("wait_agent", `{"targets":["`+strings.Join(ids(req), `","`)+`"],"timeout_ms":60000}`)}}
	}}
}

// TestDepth_ChildrenNeverSpawn pins the fixed depth: with max_depth set
// above 1, a child's request still has no spawn tools and its spawn call
// finds no such tool, and a forked child, which keeps its parent's tools,
// is refused with Codex's depth message.
func TestDepth_ChildrenNeverSpawn(t *testing.T) {
	e := newEnv(t, agents.Config{MaxDepth: 5},
		fakellm.Reply{Calls: []fakellm.Call{
			call("spawn_agent", `{"message":"CHILD-PLAIN go"}`),
			call("spawn_agent", `{"message":"CHILD-FORKED go","fork_context":true}`),
		}},
		waitAll(),
		fakellm.Reply{Text: "done"},
	)
	e.llm.Route("CHILD-PLAIN", fakellm.Reply{Calls: []fakellm.Call{call("spawn_agent", `{"message":"CHILD-GRAND no"}`)}}, fakellm.Reply{Text: "plain"})
	e.llm.Route("CHILD-FORKED", fakellm.Reply{Calls: []fakellm.Call{call("spawn_agent", `{"message":"CHILD-GRAND no"}`)}}, fakellm.Reply{Text: "forked"})
	s, ev := e.open(t, false)

	_, err := s.Submit("delegate")
	require.NoError(t, err)
	assert.Equal(t, "done", ev.finished().Answer)
	// wait_agent returns with the first child to finish; the other may still work.
	refused := func(prefix, message string) func() bool {
		return func() bool { return strings.Contains(childOutputs(e, prefix), message) }
	}
	require.Eventually(t, refused("CHILD-PLAIN", `tool "spawn_agent" is not available in this session`), waitTimeout, 10*time.Millisecond)
	require.Eventually(t, refused("CHILD-FORKED", "Agent depth limit reached. Solve the task yourself."), waitTimeout, 10*time.Millisecond)

	plain, forked := requestWith(t, e, "CHILD-PLAIN"), requestWith(t, e, "CHILD-FORKED")
	for _, name := range []string{"spawn_agent", "send_input", "wait_agent", "close_agent", "resume_agent"} {
		assert.NotContains(t, plain.ToolNames, name, "a child is never offered the spawn tools")
		assert.Contains(t, forked.ToolNames, name, "a fork keeps its parent's tools")
	}
	assert.False(t, slices.ContainsFunc(e.llm.Requests(), func(r fakellm.Request) bool { return slices.Contains(r.UserTexts, "CHILD-GRAND no") }), "no grandchild started")
}

// TestScope_Tools offers a role's agents only its tools: the registry of
// the child's run has them alone, and another tool's call fails.
func TestScope_Tools(t *testing.T) {
	roles := []agents.Role{{Name: "viewer", Description: "Views.", DeveloperInstructions: "View.", Tools: []string{"ViewImage"}}}
	e := newEnv(t, agents.Config{Roles: roles},
		fakellm.Reply{Calls: []fakellm.Call{call("spawn_agent", `{"message":"CHILD-VIEW look","agent_type":"viewer"}`), call("spawn_agent", `{"message":"CHILD-ALL look"}`)}},
		waitAll(),
		fakellm.Reply{Text: "done"},
	)
	e.llm.Route("CHILD-VIEW", fakellm.Reply{Commands: []string{"echo scoped-out"}}, fakellm.Reply{Text: "viewed"})
	e.llm.Route("CHILD-ALL", fakellm.Reply{Text: "all"})
	s, ev := e.open(t, false)

	_, err := s.Submit("delegate")
	require.NoError(t, err)
	assert.Equal(t, "done", ev.finished().Answer)

	assert.Equal(t, []string{"ViewImage"}, requestWith(t, e, "CHILD-VIEW").ToolNames)
	assert.Contains(t, requestWith(t, e, "CHILD-ALL").ToolNames, "Bash", "a child without a tools list gets every tool")
	require.Eventually(t, func() bool { return strings.Contains(childOutputs(e, "CHILD-VIEW"), `tool "Bash" is not available`) }, waitTimeout, 10*time.Millisecond)
	assert.NotContains(t, childOutputs(e, "CHILD-VIEW"), "scoped-out\n", "the command did not run")
}

// TestScope_PreApproval runs a role's pre-approved escalation without
// asking, while a forbid rule still wins over it.
func TestScope_PreApproval(t *testing.T) {
	roles := []agents.Role{{Name: "fetcher", Description: "Fetches.", DeveloperInstructions: "Fetch.", Approve: []string{"echo"}}}
	e := newEnv(t, agents.Config{Roles: roles},
		fakellm.Reply{Calls: []fakellm.Call{call("spawn_agent", `{"message":"CHILD-FETCH go","agent_type":"fetcher"}`)}},
		waitAll(),
		fakellm.Reply{Text: "done"},
	)
	e.llm.Route("CHILD-FETCH", fakellm.Reply{Escalated: []string{"echo fetched", "echo forbidden"}}, fakellm.Reply{Text: "fetched"})
	forbid, err := rules.FromPrefixes([]string{"echo forbidden"}, rules.Forbidden, "test")
	require.NoError(t, err)
	s, ev := e.open(t, true, e.sandboxed(t), func(c *embedded.Config) { c.Approver = approval.New(approval.Config{Rules: forbid}) })

	_, err = s.Submit("delegate")
	require.NoError(t, err)
	assert.Equal(t, "done", ev.finished().Answer)

	for _, x := range ev.all {
		_, asked := x.(session.ApprovalRequested)
		assert.False(t, asked, "the pre-approved command did not ask")
	}
	outputs := childOutputs(e, "CHILD-FETCH")
	assert.Contains(t, outputs, "fetched")
	assert.Contains(t, outputs, "a rule forbids this command")
}

// TestScope_PreApprovalKeepsReadOnly still asks for a pre-approved
// escalation in read only mode: a scope never widens the mode.
func TestScope_PreApprovalKeepsReadOnly(t *testing.T) {
	roles := []agents.Role{{Name: "fetcher", Description: "Fetches.", DeveloperInstructions: "Fetch.", Approve: []string{"echo"}}}
	e := newEnv(t, agents.Config{Roles: roles},
		fakellm.Reply{Calls: []fakellm.Call{call("spawn_agent", `{"message":"CHILD-RO go","agent_type":"fetcher"}`)}},
		waitAll(),
		fakellm.Reply{Text: "done"},
	)
	e.llm.Route("CHILD-RO", fakellm.Reply{Escalated: []string{"echo written"}}, fakellm.Reply{Text: "asked"})
	s, ev := e.open(t, true, e.sandboxed(t))
	_, err := s.SetSettings(e.settings().WithMode(approval.ModeReadOnly))
	require.NoError(t, err)

	_, err = s.Submit("delegate")
	require.NoError(t, err)
	req := ev.approval()
	assert.Contains(t, req.Command, "echo written")
	require.NoError(t, s.Resolve(req.ID, approval.Decline))
	assert.Equal(t, "done", ev.finished().Answer)
}
