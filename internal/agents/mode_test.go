package agents_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/agents"
	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

// TestAgents_ChildTakesTheParentsMode pins that a child starts in the
// permission mode its parent is in when it spawns, here read only chosen
// after the session opened, and that each session's sidecar keeps its own
// settings.
func TestAgents_ChildTakesTheParentsMode(t *testing.T) {
	e := newEnv(t, agents.Config{},
		fakellm.Reply{Calls: []fakellm.Call{call("spawn_agent", `{"message":"CHILD-RO look around","reasoning_effort":"low"}`)}},
		fakellm.Reply{From: func(req fakellm.Request) fakellm.Reply {
			return fakellm.Reply{Calls: []fakellm.Call{call("wait_agent", `{"targets":["`+strings.Join(ids(req), `","`)+`"],"timeout_ms":60000}`)}}
		}},
		fakellm.Reply{Text: "done"},
	)
	e.llm.Route("CHILD-RO", fakellm.Reply{Text: "looked"})
	s, ev := e.open(t, false, e.sandboxed(t))
	_, err := s.SetSettings(e.settings().WithMode(approval.ModeReadOnly))
	require.NoError(t, err)

	_, err = s.Submit("delegate")
	require.NoError(t, err)
	ev.finished()

	reqs := e.llm.Requests()
	i := slices.IndexFunc(reqs, func(r fakellm.Request) bool { return slices.Contains(r.UserTexts, "CHILD-RO look around") })
	require.GreaterOrEqual(t, i, 0)
	var defs strings.Builder
	for _, d := range reqs[i].ToolDefs {
		defs.Write(d)
	}
	assert.Contains(t, defs.String(), "Commands run in a read-only sandbox", "the child runs in the parent's mode")

	parent, found, err := session.ReadSidecar(e.sessionsDir(), s.ID())
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, parent.Settings)
	assert.Equal(t, approval.ModeReadOnly, parent.Settings.Mode)
	assert.Equal(t, "high", parent.Settings.Effort)

	childIDs := ids(lastParent(e))
	require.Len(t, childIDs, 1)
	child, found, err := session.ReadSidecar(e.sessionsDir(), childIDs[0])
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, child.Settings)
	assert.Equal(t, session.SourceSubagent, child.Source)
	assert.Equal(t, approval.ModeReadOnly, child.Settings.Mode)
	assert.Equal(t, "low", child.Settings.Effort, "the child's own effort, not the parent's")
}
