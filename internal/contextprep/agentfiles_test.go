package contextprep_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uah/internal/contextprep"
)

func TestAgentFiles(t *testing.T) {
	t.Parallel()
	got := contextprep.AgentFiles{}.Prepare(t.Context(), contextprep.Facts{InstructionFiles: []string{"/home/u/.codex/AGENTS.md", "/repo/AGENTS.md"}})
	assert.Equal(t, "Instruction files in the system prompt, in order (their @ lines are expanded in place):\n"+
		"- /home/u/.codex/AGENTS.md\n- /repo/AGENTS.md\n"+
		"These are all of the session's instruction files: there is no need to search for more AGENTS.md or CLAUDE.md files.", got)

	none := contextprep.AgentFiles{}.Prepare(t.Context(), contextprep.Facts{})
	assert.Contains(t, none, "No instruction files (AGENTS.md) were loaded")
}
