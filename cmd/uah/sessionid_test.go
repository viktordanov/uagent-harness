package main_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

// TestSessionIDFlag starts `uah run` with --session-id, finds the session
// by that ID, and refuses the ID a second time and on `uah resume`.
func TestSessionIDFlag(t *testing.T) {
	e, env := fakeEnv(t, fakellm.Reply{Text: "hello"})
	const id = "5f0c9a2e-8d4b-4c7a-9e1f-2a3b4c5d6e7f"

	res := uahWith(t, env, "", "run", "-q", "--session-id", id, "-C", e.Workspace, "first question")
	require.Equal(t, 0, res.code, res.stderr)

	show := uahWith(t, env, "", "sessions", "show", id)
	require.Equal(t, 0, show.code, show.stderr)
	assert.Contains(t, show.stdout, "session "+id)
	assert.Contains(t, show.stdout, "› first question")

	again := uahWith(t, env, "", "run", "--session-id", id, "-C", e.Workspace, "again")
	assert.Equal(t, 2, again.code)
	assert.Contains(t, again.stderr, "session "+id+" exists")

	resume := uahWith(t, env, "", "resume", "--session-id", id)
	assert.Equal(t, 2, resume.code)
	assert.Contains(t, resume.stderr, "--session-id starts a new one")
}
