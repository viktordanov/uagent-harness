package contextprep_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/internal/contextprep"
)

func TestAgentFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(text), 0o644))

		return p
	}
	agents := write("AGENTS.md", "@INC.md\n@missing.md\n")
	nested := write("sub/AGENTS.md", "@../INC.md\n@BIG.md\n")
	write("INC.md", "Use the tool for every command.")
	write("sub/BIG.md", strings.Repeat("x", 4<<10))
	always := contextprep.Skill{
		Name: "terse", Description: "Shape output. Use this skill whenever responding to ANY user message.",
		Path: write("skills/terse/SKILL.md", "---\nname: terse\ndescription: x\n---\n\nBe terse.\n"),
	}
	sometimes := contextprep.Skill{Name: "pdf", Description: "Use when the user mentions a PDF.", Path: write("skills/pdf/SKILL.md", "---\nname: pdf\n---\nPDF things.")}
	huge := contextprep.Skill{Name: "huge", Description: "Always apply this skill.", Path: write("skills/huge/SKILL.md", strings.Repeat("y", 5<<10))}
	prompt := "Base.\n\n# Project instructions\n\n## " + agents + "\n\n@INC.md\n@missing.md\n\n## " + nested + "\n\n@../INC.md\n@BIG.md\n\n## /not/a/file\n"

	got := contextprep.AgentFiles{Skills: []contextprep.Skill{always, sometimes, huge}}.Prepare(t.Context(), contextprep.Facts{SystemPrompt: prompt})
	assert.Contains(t, got, "Instruction files in the system prompt, in order:\n- "+agents+"\n- "+nested+"\nThese are all")
	assert.NotContains(t, got, "/not/a/file")
	assert.Contains(t, got, agents+" includes @INC.md ("+filepath.Join(dir, "INC.md")+"), so you need not read it:\nUse the tool for every command.")
	assert.Equal(t, 1, strings.Count(got, "Use the tool for every command."), "an include named twice is inlined once")
	assert.Contains(t, got, "too long to show here; read them when you need them: "+filepath.Join(dir, "sub", "BIG.md"))
	assert.Contains(t, got, "inlined here so you need not load them: terse.\n\n### Skill terse ("+always.Path+")\nBe terse.")
	assert.NotContains(t, got, "PDF things.")
	assert.Contains(t, got, "too long to inline; load them: huge.")

	none := contextprep.AgentFiles{}.Prepare(t.Context(), contextprep.Facts{SystemPrompt: "Base."})
	assert.Equal(t, "No instruction files (AGENTS.md) were loaded for this session, so there are none to search for.", none)
}

func TestAppliesAlways(t *testing.T) {
	for desc, want := range map[string]bool{
		"Use this skill whenever responding to ANY user message including coding tasks.": true,
		"Apply on every response.":                          true,
		"Always apply these rules.":                         true,
		"This skill is always active.":                      true,
		"Use for each turn of the conversation.":            true,
		"Use when the user mentions a PDF.":                 false,
		"Auto-invoke when reviewing Go code.":               false,
		"Always prefer small functions when writing Go.":    false,
		"Covers messages, responses, and replies in Slack.": false,
	} {
		assert.Equal(t, want, contextprep.AppliesAlways(desc), desc)
	}
}
