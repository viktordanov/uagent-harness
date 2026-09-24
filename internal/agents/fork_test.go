package agents_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/agents"
	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

// requestWith is the first request with a user message starting with
// prefix.
func requestWith(t *testing.T, e *env, prefix string) fakellm.Request {
	t.Helper()
	reqs := e.llm.Requests()
	i := slices.IndexFunc(reqs, func(r fakellm.Request) bool {
		return slices.ContainsFunc(r.UserTexts, func(u string) bool { return strings.HasPrefix(u, prefix) })
	})
	require.GreaterOrEqual(t, i, 0, "no request with %q", prefix)

	return reqs[i]
}

// parentRequest is the parent's n-th model request (0 is the first).
func parentRequest(t *testing.T, e *env, n int) fakellm.Request {
	t.Helper()
	var parent []fakellm.Request
	for _, r := range e.llm.Requests() {
		if !isChild(r) {
			parent = append(parent, r)
		}
	}
	require.Greater(t, len(parent), n)

	return parent[n]
}

// assertSharedPrefix checks the cache evidence: the child's first request
// has the parent's system prompt and tools, in order and byte for byte,
// the parent's prompt cache key, and starts with every input item of the
// parent's request that made the spawn call, followed by the child's
// message.
func assertSharedPrefix(t *testing.T, parent, child fakellm.Request, message string) {
	t.Helper()
	assert.Equal(t, parent.System, child.System, "the same system prompt")
	assert.Equal(t, parent.ToolNames, child.ToolNames, "the same tools in the same order")
	require.Len(t, child.ToolDefs, len(parent.ToolDefs))
	for i := range parent.ToolDefs {
		assert.JSONEq(t, string(parent.ToolDefs[i]), string(child.ToolDefs[i]), "tool %s", parent.ToolNames[i])
	}
	assert.NotEmpty(t, parent.CacheKey)
	assert.Equal(t, parent.CacheKey, child.CacheKey, "the child's requests use its tree's cache key")
	require.Greater(t, len(child.Input), len(parent.Input), "the child's request extends the parent's")
	for i := range parent.Input {
		assert.JSONEq(t, string(parent.Input[i]), string(child.Input[i]), "input item %d", i)
	}
	assert.Contains(t, string(child.Input[len(parent.Input)]), message, "the child's message follows the parent's items")
}

// TestFork_ChildStartsWithTheParentsRequest forks a child that then shares
// the longest prefix with the parent's request that spawned it: the same
// system prompt, the same tools in the same order (the spawn tools stay
// offered at the depth limit, and its own spawn is refused), and the
// parent's items, tool calls and results included.
func TestFork_ChildStartsWithTheParentsRequest(t *testing.T) {
	e := newEnv(t, agents.Config{},
		fakellm.Reply{Commands: []string{"echo parent-work"}},
		fakellm.Reply{Calls: []fakellm.Call{call("spawn_agent", `{"message":"CHILD-FORK go on from here","fork_context":true}`)}},
		callWith("wait_agent", `{"targets":["ID"]}`),
		fakellm.Reply{Text: "done"},
	)
	e.llm.Route("CHILD-FORK",
		fakellm.Reply{Calls: []fakellm.Call{call("spawn_agent", `{"message":"CHILD-GRAND no"}`)}},
		fakellm.Reply{Text: "forked answer"},
	)
	s, ev := e.open(t, false)

	_, err := s.Submit("look around")
	require.NoError(t, err)
	assert.Equal(t, "done", ev.finished().Answer)

	parent, child := parentRequest(t, e, 1), requestWith(t, e, "CHILD-FORK") // the second request made the spawn call
	assert.Contains(t, strings.Join(parent.ToolOutputs, "\n"), "parent-work", "the parent's request has a tool result")
	assertSharedPrefix(t, parent, child, "CHILD-FORK go on from here")
	assert.Contains(t, child.ToolNames, "spawn_agent", "a forked child keeps the spawn tools")
	assert.Contains(t, childOutputs(e, "CHILD-FORK"), "Agent depth limit reached. Solve the task yourself.")
	assert.Contains(t, lastOutputs(e), `{"completed":"forked answer"}`)
}

// TestFork_KeepsTheParentsCompaction forks a parent whose context was
// compacted: the child's first request is compacted the same way.
func TestFork_KeepsTheParentsCompaction(t *testing.T) {
	e := newEnv(t, agents.Config{},
		fakellm.Reply{Text: "hello back"},
		fakellm.Reply{Text: "SUMMARY of the talk"},
		fakellm.Reply{Calls: []fakellm.Call{call("spawn_agent", `{"message":"CHILD-COMPACT continue","fork_context":true}`)}},
		callWith("wait_agent", `{"targets":["ID"]}`),
		fakellm.Reply{Text: "done"},
	)
	e.llm.Route("CHILD-COMPACT", fakellm.Reply{Text: "continued"})
	s, ev := e.open(t, false)
	_, err := s.Submit("hello")
	require.NoError(t, err)
	ev.finished()
	require.NoError(t, s.Compact())
	_, err = s.Submit("delegate")
	require.NoError(t, err)
	assert.Equal(t, "done", ev.finished().Answer)

	parent, child := parentRequest(t, e, 2), requestWith(t, e, "CHILD-COMPACT") // after the answer and the summary
	assert.Contains(t, strings.Join(parent.UserTexts, "\n"), "SUMMARY of the talk", "the parent's request was compacted")
	assertSharedPrefix(t, parent, child, "CHILD-COMPACT continue")
}

// TestFork_RefusesAnAgentType keeps Codex's rule: a fork inherits the
// parent's agent type.
func TestFork_RefusesAnAgentType(t *testing.T) {
	roles := []agents.Role{{Name: "reviewer", Description: "Reviews.", DeveloperInstructions: "Review."}}
	e := newEnv(t, agents.Config{Roles: roles},
		fakellm.Reply{Calls: []fakellm.Call{call("spawn_agent", `{"message":"CHILD-X","fork_context":true,"agent_type":"reviewer"}`)}},
		fakellm.Reply{Text: "done"},
	)
	s, ev := e.open(t, false)
	_, err := s.Submit("fork")
	require.NoError(t, err)
	ev.finished()
	assert.Contains(t, lastOutputs(e), "Full-history forked agents inherit the parent agent type")
}

// TestFork_ResumedForkKeepsItsTools offers a forked child, resumed from its
// files at the depth limit, the tools its parent had; a plain child at the
// same depth gets none, as in Codex.
func TestFork_ResumedForkKeepsItsTools(t *testing.T) {
	dir := t.TempDir()
	m := agents.New(agents.Config{MaxDepth: 1})
	m.Bind(nil, session.Options{SessionsDir: dir})
	for id, rec := range map[string]string{"forked": `{"nickname":"Ada","fork":true}`, "plain": `{"nickname":"Babbage"}`} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, id+".uah.json"), []byte(`{"source":"subagent","parent":"root"}`), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, id+".agent.json"), []byte(rec), 0o600))
	}

	assert.Len(t, m.Attach(engine.AgentParent{SessionID: "forked"}), 5)
	assert.Empty(t, m.Attach(engine.AgentParent{SessionID: "plain"}))
}
