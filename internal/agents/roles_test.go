package agents_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/agents"
)

func writeRole(t *testing.T, dir, name, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
}

func TestLoadRoles(t *testing.T) {
	user, project := t.TempDir(), t.TempDir()
	writeRole(t, user, "reviewer.toml", `
name = "reviewer"
description = "Reviews diffs."
nickname_candidates = ["Rex", "Rita"]
model = "gpt-small"
model_reasoning_effort = "low"
developer_instructions = "Review only."
sandbox_mode = "read-only"
`)
	writeRole(t, user, "worker.toml", `
name = "worker"
description = "User worker."
developer_instructions = "Work."
`)
	writeRole(t, project, "worker.toml", `
name = "worker"
description = "Project worker."
developer_instructions = "Work here."
`)
	writeRole(t, project, "bad.toml", `name = "bad"`)
	writeRole(t, project, "notes.txt", `ignored`)

	roles, warnings := agents.LoadRoles(user, project, filepath.Join(user, "missing"))

	require.Len(t, roles, 2)
	assert.Equal(t, "reviewer", roles[0].Name)
	assert.Equal(t, []string{"Rex", "Rita"}, roles[0].NicknameCandidates)
	assert.Equal(t, "gpt-small", roles[0].Model)
	assert.Equal(t, "low", roles[0].Effort)
	assert.Equal(t, "Project worker.", roles[1].Description, "the project role replaces the user role")
	require.Len(t, warnings, 2)
	assert.Contains(t, warnings[0], "ignoring keys uah does not support: sandbox_mode")
	assert.Contains(t, warnings[1], "must define a non-empty description")
}
