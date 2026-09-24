package engine_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent-harness/internal/engine"
)

// TestCapabilities_Table: an engine without a capability lacks every
// feature that needs it, and only the used ones get a notice, in the
// table's order.
func TestCapabilities_Table(t *testing.T) {
	caps := engine.Capabilities{Rules: true}
	used := []engine.Feature{engine.FeatureMCP, engine.FeatureRules, engine.FeaturePromptRules}
	unsupported := caps.Unsupported(used)
	assert.Len(t, unsupported, 2, "the rules run")
	assert.Equal(t, engine.FeaturePromptRules, unsupported[0].Feature)
	assert.Equal(t, "MCP servers: not supported by the process engine (they do not start); use the embedded engine", unsupported[1].Notice("process"))
	assert.NotContains(t, caps.Summary(), "command rules")
	assert.Contains(t, caps.Summary(), "prompt rules")

	all := engine.Capabilities{
		LiveInput: true, LiveEffort: true, LiveModel: true, ServiceTier: true, Compaction: true, LiveMode: true, Rules: true,
		Approvals: true, ToolHooks: true, MCP: true, Subagents: true, ApplyPatch: true, CodexSkills: true, ContextUsage: true,
	}
	assert.Empty(t, all.Lacks())
	assert.Empty(t, all.Summary())
	all.LiveMode = false
	assert.Equal(t, "live settings", all.Summary(), "live settings need effort, model, and mode")
}
