package agents_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/agents"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

// TestAgents_ModelEffortAndFastPerChild runs children on the parent's
// provider with their own settings: a role with service_tier = "priority"
// (fast mode), and a spawn call with its own model and effort. /context
// and the compaction window follow the child's model.
func TestAgents_ModelEffortAndFastPerChild(t *testing.T) {
	roles := []agents.Role{{Name: "fast-reviewer", Description: "Reviews fast.", ServiceTier: agents.TierPriority, DeveloperInstructions: "Review."}}
	e := newEnv(t, agents.Config{Roles: roles, MaxThreads: 2},
		fakellm.Reply{Calls: []fakellm.Call{
			call("spawn_agent", `{"message":"CHILD-FAST review","agent_type":"fast-reviewer"}`),
			call("spawn_agent", `{"message":"CHILD-MINI count","model":"gpt-mini","reasoning_effort":"low"}`),
		}},
		fakellm.Reply{From: func(req fakellm.Request) fakellm.Reply {
			return fakellm.Reply{Calls: []fakellm.Call{call("wait_agent", `{"targets":["`+strings.Join(ids(req), `","`)+`"],"timeout_ms":60000}`)}}
		}},
		fakellm.Reply{Text: "reviewed and counted"},
	)
	e.llm.Route("CHILD-FAST", fakellm.Reply{Text: "looks fine"})
	e.llm.Route("CHILD-MINI", fakellm.Reply{Text: "seven"})
	s, ev := e.open(t, false)

	_, err := s.Submit("delegate")
	require.NoError(t, err)
	ev.finished()

	byText := func(prefix string) fakellm.Request {
		reqs := e.llm.Requests()
		i := slices.IndexFunc(reqs, func(r fakellm.Request) bool {
			return slices.ContainsFunc(r.UserTexts, func(u string) bool { return strings.HasPrefix(u, prefix) })
		})
		require.GreaterOrEqual(t, i, 0, "no request from %s", prefix)

		return reqs[i]
	}
	parent, fast, mini := byText("delegate"), byText("CHILD-FAST"), byText("CHILD-MINI")
	assert.Equal(t, "gpt-test", parent.Model)
	assert.Empty(t, parent.ServiceTier)
	assert.Equal(t, "gpt-test", fast.Model, "a role without a model keeps the parent's")
	assert.Equal(t, "priority", fast.ServiceTier, "the role's service_tier turns on fast mode for its agents only")
	assert.Equal(t, "gpt-mini", mini.Model, "same provider, another model")
	assert.Equal(t, "low", mini.Effort)
	assert.Empty(t, mini.ServiceTier)

	var models []string
	for _, id := range ids(lastParent(e)) {
		u, ok := e.mgr.ContextUsage(id)
		require.True(t, ok, "/context works for a child")
		models = append(models, u.Model)
	}
	assert.ElementsMatch(t, []string{"gpt-test", "gpt-mini"}, models, "/context and the compaction window use each child's model")
}
