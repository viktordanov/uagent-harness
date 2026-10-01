package state_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent/core"
)

// TestReduce_PatchLabel: an apply_patch line names the files its patch
// changes, from a call's raw patch or the {"input": patch} of a call
// recorded when apply_patch was a function tool.
func TestReduce_PatchLabel(t *testing.T) {
	for _, args := range []string{
		`{"input":"*** Begin Patch\n*** Update File: a.go\n@@\n-x\n+y\n*** Add File: b.go\n+z\n*** End Patch"}`,
		"*** Begin Patch\n*** Update File: a.go\n@@\n-x\n+y\n*** Add File: b.go\n+z\n*** End Patch\n",
	} {
		s, _ := apply(opened(), core.ToolCalled{At: t0, CallID: "c1", Name: "apply_patch", Label: args, Arguments: args})
		assert.Equal(t, "a.go, b.go", s.Items[len(s.Items)-1].Label)
	}
}
