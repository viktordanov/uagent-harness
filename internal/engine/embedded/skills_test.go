package embedded_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/internal/config"
	"github.com/viktordanov/uah/internal/systemskills"
	"github.com/viktordanov/uah/testing/fakellm"
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

// TestEmbedded_SystemSkills offers uah's system skills, written under
// ~/.uah/skills/.system, after every other folder: a skill of the same
// name elsewhere replaces one.
func TestEmbedded_SystemSkills(t *testing.T) {
	e := newEnv(t, fakellm.Reply{Text: "ok"})
	s, ev := e.open(t, e.embedded(), "")
	_, err := s.Submit("hi")
	require.NoError(t, err)
	ev.finished()

	req := e.llm.Requests()[0]
	assert.Contains(t, req.Tools, "SkillUse", "the system skills enable the skill tool")
	assert.Contains(t, req.System+req.Tools["SkillUse"], "uah-customization")
	want, err := systemskills.File("uah-customization")
	require.NoError(t, err)
	got, err := os.ReadFile(filepath.Join(config.Dir(), "skills", ".system", "uah-customization", "SKILL.md"))
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got), "SkillUse reads the skill from this file")

	e2 := newEnv(t, fakellm.Reply{Text: "ok"})
	writeSkill(t, filepath.Join(e2.CodexHome, "skills"), "uah-customization", "Notes on uah from the user")
	s, ev = e2.open(t, e2.embedded(), "")
	_, err = s.Submit("hi")
	require.NoError(t, err)
	ev.finished()
	req = e2.llm.Requests()[0]
	assert.Contains(t, req.System+req.Tools["SkillUse"], "Notes on uah from the user")
	assert.NotContains(t, req.System+req.Tools["SkillUse"], "How uah itself works", "the user's skill replaces the system skill")
}
