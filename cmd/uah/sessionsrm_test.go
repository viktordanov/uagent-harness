package main_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/harness"

	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

// TestSessionsRm deletes a session by prefix: --dry-run lists the paths
// and removes nothing, a held lock refuses without --force, and --json
// prints what went.
func TestSessionsRm(t *testing.T) {
	e, env := fakeEnv(t, fakellm.Reply{Text: "hello"})
	const id = "7a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"
	res := uahWith(t, env, "", "run", "-q", "--session-id", id, "-C", e.Workspace, "first question")
	require.Equal(t, 0, res.code, res.stderr)

	dry := uahWith(t, env, "", "sessions", "rm", "--dry-run", "7a1b")
	require.Equal(t, 0, dry.code, dry.stderr)
	assert.Contains(t, dry.stdout, filepath.Join(e.StateDir, "sessions", id+".session.jsonl"))
	assert.Contains(t, dry.stdout, filepath.Join(e.StateDir, "runs")+string(filepath.Separator))
	assert.Contains(t, dry.stderr, "would remove session "+id)
	assert.FileExists(t, filepath.Join(e.StateDir, "sessions", id+".session.jsonl"))

	unlock, err := harness.LockSession(e.StateDir, id)
	require.NoError(t, err)
	busy := uahWith(t, env, "", "sessions", "rm", id)
	assert.Equal(t, 1, busy.code)
	assert.Contains(t, busy.stderr, "--force removes it anyway")
	require.NoError(t, unlock())

	rm := uahWith(t, env, "", "sessions", "rm", "--json", id)
	require.Equal(t, 0, rm.code, rm.stderr)
	var out struct {
		Sessions []string `json:"sessions"`
		Paths    []string `json:"paths"`
		DryRun   bool     `json:"dry_run"`
	}
	require.NoError(t, json.Unmarshal([]byte(rm.stdout), &out))
	assert.Equal(t, []string{id}, out.Sessions)
	assert.False(t, out.DryRun)
	assert.Equal(t, strings.Split(strings.TrimSpace(dry.stdout), "\n"), out.Paths, "what the dry run listed")
	for _, p := range out.Paths {
		assert.NoFileExists(t, p)
		assert.NoDirExists(t, p)
	}

	list := uahWith(t, env, "", "sessions", "--all", "--json")
	require.Equal(t, 0, list.code, list.stderr)
	assert.JSONEq(t, "[]", list.stdout)
	again := uahWith(t, env, "", "sessions", "rm", id)
	assert.Equal(t, 2, again.code)
	assert.Contains(t, again.stderr, "no session matches")
}
