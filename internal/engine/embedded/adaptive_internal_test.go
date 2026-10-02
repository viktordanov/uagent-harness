package embedded

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/unreal-agent/harness/llm"
)

func TestAdaptiveRoute(t *testing.T) {
	msg := func(role llm.Role) llm.Item {
		return llm.Item{Type: llm.ItemMessage, Data: llm.Message{Role: role, Text: "x"}}
	}
	call := llm.Item{Type: llm.ItemToolCall, Data: llm.ToolCall{CallID: "c"}}
	result := llm.Item{Type: llm.ItemToolResult, Data: llm.ToolResult{CallID: "c"}}
	reasoning := llm.Item{Type: llm.ItemReasoning}
	system, user, assistant := msg(llm.RoleSystem), msg(llm.RoleUser), msg(llm.RoleAssistant)

	tests := []struct {
		name  string
		input []llm.Item
		want  effortChoice
	}{
		{"the first request", []llm.Item{system, user}, effortChoice{effort: "high", reason: "1-step: first request"}},
		{"results only", []llm.Item{system, user, reasoning, call, result}, effortChoice{effort: "medium", reason: "1-step: tool results only"}},
		{"several results after text", []llm.Item{system, user, assistant, call, result, result}, effortChoice{effort: "medium", reason: "1-step: tool results only"}},
		{"a user message with the results", []llm.Item{system, user, call, result, user}, effortChoice{effort: "high", reason: "1-step: user message"}},
		{"a steer before the results", []llm.Item{system, user, call, user, result}, effortChoice{effort: "high", reason: "1-step: user message"}},
		{"the model's text last", []llm.Item{system, user, assistant}, effortChoice{effort: "high", reason: "1-step: no tool results"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, adaptiveRouter{steps: 1}.route(tt.input, llm.ReasoningEffortHigh, false))
		})
	}
	two := adaptiveRouter{steps: 2}.route([]llm.Item{system, user, call, result}, llm.ReasoningEffortMax, true)
	assert.Equal(t, effortChoice{effort: llm.ReasoningEffortXHigh, reason: "2-steps: tool results only"}, two, "ultra, two steps down, on the plain client")
	kept := adaptiveRouter{steps: 2}.route([]llm.Item{system, user}, llm.ReasoningEffortMax, true)
	assert.Equal(t, effortChoice{effort: llm.ReasoningEffortMax, ultra: true, reason: "2-steps: first request"}, kept)
}

func TestLowerEffort(t *testing.T) {
	for in, want := range map[llm.ReasoningEffort][2]llm.ReasoningEffort{
		"low": {"low", "low"}, "medium": {"low", "low"}, "high": {"medium", "low"}, "xhigh": {"high", "medium"}, "max": {"xhigh", "high"},
	} {
		assert.Equal(t, want[0], lowerEffort(in, false, 1), in)
		assert.Equal(t, want[1], lowerEffort(in, false, 2), in)
	}
	assert.Equal(t, llm.ReasoningEffortMax, lowerEffort(llm.ReasoningEffortMax, true, 1), "ultra")
	assert.Equal(t, llm.ReasoningEffortXHigh, lowerEffort(llm.ReasoningEffortMax, true, 2), "ultra, two steps")
}
