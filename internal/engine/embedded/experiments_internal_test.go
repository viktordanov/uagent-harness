package embedded

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/unreal-agent/harness/llm"
)

func TestReadExperiments(t *testing.T) {
	x := readExperiments(func(string) string { return " preamble-wake,unknown, effort-by-turn,primed-first-turn" })
	assert.Equal(t, experiments{preambleWake: true, effortByTurn: true, primedFirstTurn: true}, x)
	assert.Equal(t, experiments{}, readExperiments(func(string) string { return "" }))
}

func TestContinuation(t *testing.T) {
	msg := func(role llm.Role) llm.Item {
		return llm.Item{Type: llm.ItemMessage, Data: llm.Message{Role: role, Text: "x"}}
	}
	call := llm.Item{Type: llm.ItemToolCall, Data: llm.ToolCall{CallID: "c"}}
	result := llm.Item{Type: llm.ItemToolResult, Data: llm.ToolResult{CallID: "c"}}
	reasoning := llm.Item{Type: llm.ItemReasoning}

	assert.False(t, continuation([]llm.Item{msg(llm.RoleSystem), msg(llm.RoleUser)}), "the first request")
	assert.True(t, continuation([]llm.Item{msg(llm.RoleUser), reasoning, call, result}))
	assert.True(t, continuation([]llm.Item{msg(llm.RoleUser), msg(llm.RoleAssistant), call, result, result}))
	assert.False(t, continuation([]llm.Item{msg(llm.RoleUser), call, result, msg(llm.RoleUser)}), "a user message came with the results")
	assert.False(t, continuation([]llm.Item{msg(llm.RoleUser), call, msg(llm.RoleUser), result}), "a steer before the results")
	assert.False(t, continuation([]llm.Item{msg(llm.RoleUser), msg(llm.RoleAssistant)}))
}

func TestLowerEffort(t *testing.T) {
	for in, want := range map[llm.ReasoningEffort]llm.ReasoningEffort{
		llm.ReasoningEffortLow: llm.ReasoningEffortLow, llm.ReasoningEffortMedium: llm.ReasoningEffortLow,
		llm.ReasoningEffortHigh: llm.ReasoningEffortMedium, llm.ReasoningEffortXHigh: llm.ReasoningEffortHigh,
		llm.ReasoningEffortMax: llm.ReasoningEffortXHigh,
	} {
		assert.Equal(t, want, lowerEffort(in, false), in)
	}
	assert.Equal(t, llm.ReasoningEffortMax, lowerEffort(llm.ReasoningEffortMax, true), "ultra")
}
