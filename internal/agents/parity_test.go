package agents_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/agents"
	"github.com/viktordanov/uagent-harness/internal/hooks"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

// TestParity_ChildOptions pins that a child opens with the options the
// root session opened with, differing only in its ID, its sidecar's source
// and parent, approvals through the parent, and a hook runner of its own
// with the same hooks. The settings are the parent run's.
func TestParity_ChildOptions(t *testing.T) {
	runner, err := hooks.New([]hooks.Hook{{Event: hooks.Stop, Command: "true", Source: hooks.SourceUser}}, nil, "")
	require.NoError(t, err)
	e := newEnv(t, agents.Config{}, fakellm.Reply{Text: "done"})
	e.hooks = runner
	s, ev := e.open(t, true)
	_, err = s.Submit("hello")
	require.NoError(t, err)
	ev.finished()

	root := session.Options{
		Settings: e.settings(), Hooks: runner, SessionsDir: e.sessionsDir(), Source: session.SourceTUI, Interactive: true,
	}
	child := e.mgr.ChildOptions(s.ID())

	assert.Equal(t, "child-id", child.ID)
	assert.Equal(t, session.SourceSubagent, child.Source)
	assert.Equal(t, s.ID(), child.Parent)
	assert.NotNil(t, child.Ask, "approvals go through the parent")
	assert.NotSame(t, runner, child.Hooks, "a runner of its own, so its results stay its own")
	assert.Equal(t, runner.Hooks(), child.Hooks.Hooks())
	child.ID, child.Source, child.Parent, child.Ask, child.Hooks = root.ID, root.Source, root.Parent, root.Ask, root.Hooks
	assert.Equal(t, root, child)
}
