package state

import (
	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/patch"
)

// callLabel is a tool call's label: the files an apply_patch call changes,
// the subagents' nicknames for the agent tools, else the runner's label.
func (s *State) callLabel(e core.ToolCalled) string {
	if e.Name == patch.ToolName {
		if files := patch.Describe(e.Arguments); files != "" {
			return files
		}
	}

	return s.agentCallLabel(e.Name, e.Label)
}

// onPatchApplied puts an applied patch's diff on its tool call, which the
// views draw under the call's line.
func (s *State) onPatchApplied(e engine.PatchApplied) {
	if !s.update("call:"+e.CallID, func(it *Item) { it.Diff = e.Files }) {
		s.put(Item{Kind: KindTool, Key: "call:" + e.CallID, Name: patch.ToolName, Tool: ToolOK, Diff: e.Files})
	}
}
