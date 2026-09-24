package state

import (
	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/patch"
)

// eventLabel is a tool call's label: the files an apply_patch call
// changes, else callLabel's.
func (s *State) eventLabel(e core.ToolCalled) string {
	if e.Name == patch.ToolName {
		if files := patch.Describe(e.Arguments); files != "" {
			return files
		}
	}

	return s.callLabel(e.Name, e.Label)
}

// onPatchApplied puts an applied patch's diff on its tool call, which the
// views draw under the call's line.
func (s *State) onPatchApplied(e engine.PatchApplied) {
	if !s.update("call:"+e.CallID, func(it *Item) { it.Diff = e.Files }) {
		s.put(Item{Kind: KindTool, Key: "call:" + e.CallID, Name: patch.ToolName, Tool: ToolOK, Diff: e.Files})
	}
}
