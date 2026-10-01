package embedded

import (
	"slices"

	"github.com/viktordanov/unreal-agent/harness/llm"
)

// The effort-by-turn experiment: a request that only continues after tool
// results, with no user message since the model's last output, runs one
// effort level lower than configured. The first request and every request
// that carries a user message keep the configured effort. The switcher
// applies it in Respond, so it holds for every turn request and for no
// direct call (compaction summaries, the auto-reviewer).

// continuation reports whether input ends with tool results and nothing
// else since the model's last output item.
func continuation(input []llm.Item) bool {
	results := false
	for _, it := range slices.Backward(input) {
		switch it.Type {
		case llm.ItemToolResult:
			results = true
		case llm.ItemMessage:
			m, _ := it.Data.(llm.Message)

			return results && m.Role == llm.RoleAssistant
		default: // the model's tool call or reasoning
			return results
		}
	}

	return false
}

// lowerEffort is the effort one level below effort, never below low; at
// ultra (the runner's max with the ultra client) it is max.
func lowerEffort(effort llm.ReasoningEffort, ultra bool) llm.ReasoningEffort {
	if ultra {
		return llm.ReasoningEffortMax
	}
	switch effort {
	case llm.ReasoningEffortMax:
		return llm.ReasoningEffortXHigh
	case llm.ReasoningEffortXHigh:
		return llm.ReasoningEffortHigh
	case llm.ReasoningEffortHigh:
		return llm.ReasoningEffortMedium
	case llm.ReasoningEffortMedium, llm.ReasoningEffortLow:
		return llm.ReasoningEffortLow
	default:
		return effort
	}
}
