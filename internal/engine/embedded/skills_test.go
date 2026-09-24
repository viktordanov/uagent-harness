package embedded_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

func writeSkill(t *testing.T, root, name, description string) {
	t.Helper()
	dir := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"),
		[]byte("---\nname: "+name+"\ndescription: "+description+"\n---\n\nSteps.\n"), 0o600))
}

// TestEmbedded_CodexSkills offers skills from Codex's places: the project's
// .agents/skills wins over $CODEX_HOME/skills for the same name.
func TestEmbedded_CodexSkills(t *testing.T) {
	e := newEnv(t, fakellm.Reply{Text: "ok"})
	writeSkill(t, filepath.Join(e.Workspace, ".agents", "skills"), "release", "Cut a release from the project")
	writeSkill(t, filepath.Join(e.CodexHome, "skills"), "release", "The user's release steps")
	writeSkill(t, filepath.Join(e.CodexHome, "skills"), "triage", "Triage an issue")
	s, ev := e.open(t, e.embedded(), "")

	_, err := s.Submit("hi")
	require.NoError(t, err)
	ev.finished()

	req := e.llm.Requests()[0]
	assert.Contains(t, req.Tools, "SkillUse", "skills enable the runner's skill tool")
	assert.Contains(t, req.System+req.Tools["SkillUse"], "Cut a release from the project")
	assert.NotContains(t, req.System+req.Tools["SkillUse"], "The user's release steps")
	assert.Contains(t, req.System+req.Tools["SkillUse"], "Triage an issue")
}
