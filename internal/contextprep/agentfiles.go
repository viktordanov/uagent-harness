package contextprep

import (
	"context"
	"strings"
)

// AgentFiles is the "agent files" block: the instruction files the system
// prompt holds, said to be all of them, so the model does not search for
// more.
type AgentFiles struct{}

// Name is the block's name.
func (AgentFiles) Name() string { return "agent files" }

// Prepare lists the instruction files.
func (AgentFiles) Prepare(_ context.Context, f Facts) string {
	if len(f.InstructionFiles) == 0 {
		return "No instruction files (AGENTS.md) were loaded for this session, so there are none to search for."
	}

	return "Instruction files in the system prompt, in order (their @ lines are expanded in place):\n- " +
		strings.Join(f.InstructionFiles, "\n- ") +
		"\nThese are all of the session's instruction files: there is no need to search for more AGENTS.md or CLAUDE.md files."
}
