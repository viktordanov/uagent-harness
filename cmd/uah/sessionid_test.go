package main_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

// TestSessionIDFlag starts `uah exec` with --session-id, finds the session
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

// TestNeverUsedSession lists a session that has a sidecar and no session
// file, as a launch stopped before its first message leaves it, and resumes
// it under its ID with `uah exec --session`, which then records its history.
func TestNeverUsedSession(t *testing.T) {
	e, env := fakeEnv(t, fakellm.Reply{Text: "hello"})
	const id = "6a0c9a2e-8d4b-4c7a-9e1f-2a3b4c5d6e7f"
	sidecar := `{"source":"tui","created":"2026-09-30T00:00:00Z","workspace":` + strconv.Quote(e.Workspace) + "}\n"
	require.NoError(t, os.MkdirAll(filepath.Join(e.StateDir, "sessions"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(e.StateDir, "sessions", id+".uah.json"), []byte(sidecar), 0o600))

	list := uahWith(t, env, "", "sessions", "--json", "-C", e.Workspace)
	require.Equal(t, 0, list.code, list.stderr)
	var infos []map[string]any
	require.NoError(t, json.Unmarshal([]byte(list.stdout), &infos))
	require.Len(t, infos, 1)
	assert.Equal(t, id, infos[0]["ID"])
	assert.InDelta(t, 0, infos[0]["Runs"], 0)
	assert.InDelta(t, 0, infos[0]["LastSequence"], 0)
	assert.Empty(t, infos[0]["FirstPrompt"])
	assert.Equal(t, "2026-09-30T00:00:00Z", infos[0]["LastActivity"], "active when it was created")

	res := uahWith(t, env, "", "run", "-q", "--session", id, "first question")
	require.Equal(t, 0, res.code, res.stderr)
	show := uahWith(t, env, "", "sessions", "show", id)
	require.Equal(t, 0, show.code, show.stderr)
	assert.Contains(t, show.stdout, "› first question")
	assert.FileExists(t, filepath.Join(e.StateDir, "sessions", id+".session.jsonl"))
}
