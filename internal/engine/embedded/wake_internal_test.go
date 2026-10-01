package embedded

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/unreal-agent/harness/llm"
	"github.com/viktordanov/unreal-agent/harness/tool"
)

func TestWakeExperimentsSetTheWakePolicy(t *testing.T) {
	off := readExperiments(func(string) string { return "other" }).wakePolicy()
	assert.False(t, off.Batch || off.AllDone || off.Debounce != 0 || off.Yield != nil || off.Progress != nil, "no wake experiment keeps the default policy")

	on := readExperiments(func(string) string {
		return "wake-no-placeholder, wake-debounce,wake-foreground,wake-all-done"
	}).wakePolicy()
	assert.True(t, on.Batch)
	assert.True(t, on.AllDone)
	assert.Equal(t, wakeDebounce, on.Debounce)
	assert.NotNil(t, on.Yield)
	assert.NotNil(t, on.Progress)
	assert.Equal(t, foregroundYield, on.Yield(llm.ToolCall{Name: tool.BashName, Arguments: `{}`}))

	long := readExperiments(func(string) string { return "wake-foreground-long" }).wakePolicy()
	assert.Equal(t, longForegroundYield, long.Yield(llm.ToolCall{Name: tool.BashName, Arguments: `{}`}))
}

func TestBashYieldWaitsUnlessInTheBackground(t *testing.T) {
	for args, want := range map[string]time.Duration{
		`{"command":"go test ./..."}`:                    foregroundYield,
		`{"command":"go test ./...","background":false}`: foregroundYield,
		`{"command":"npm run dev","background":true}`:    0,
		`not json`: foregroundYield,
	} {
		assert.Equal(t, want, bashYield(foregroundYield)(llm.ToolCall{Name: tool.BashName, Arguments: args}), args)
	}
	assert.Zero(t, bashYield(foregroundYield)(llm.ToolCall{Name: "apply_patch", Arguments: `{}`}), "only Bash holds the turn")
}

func TestBashForegroundKeepsTheBaseDefinition(t *testing.T) {
	base := llm.Tool{Name: tool.BashName, Parameters: map[string]any{
		"type":       "object",
		"properties": map[string]any{"command": map[string]any{"type": "string"}},
	}}
	got := bashForeground(base, longForegroundYield)
	assert.Contains(t, got.Parameters["properties"], "background")
	assert.NotContains(t, base.Parameters["properties"], "background", "the base definition is not changed")
	assert.Contains(t, got.Description, "up to 5 minutes;")
}
