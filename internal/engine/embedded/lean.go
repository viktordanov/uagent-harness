package embedded

import (
	"fmt"
	"slices"

	"github.com/viktordanov/unreal-agent/harness/llm"
)

// Lean mode's effort routing: a request whose input since the model's last
// output is only tool results goes the mode's steps (1 or 2) below the
// effort the user picked, never below low. The first request and every
// request that carries a user message go at the user's effort. A
// benchmark of the rules (docs/design/agent-tuning.md) kept this one:
// lowering only some follow-ups changed the effort between requests more
// often, which costs the prompt cache more than the lower effort saves.

// leanRouter picks each turn request's effort.
type leanRouter struct {
	// steps is how many levels a follow-up goes down: 1 or 2.
	steps int
}

// effortChoice is a request's effort: the runner's level, ultra when the
// ultra client sends it, and why.
type effortChoice struct {
	effort llm.ReasoningEffort
	ultra  bool
	reason string
}

// route picks the effort for a request at e (ultra: at effort ultra).
func (l leanRouter) route(input []llm.Item, e llm.ReasoningEffort, ultra bool) effortChoice {
	tag := fmt.Sprintf("%d-steps: ", l.steps)
	if l.steps == 1 {
		tag = "1-step: "
	}
	if why := userTurn(input); why != "" {
		return effortChoice{effort: e, ultra: ultra, reason: tag + why}
	}

	return effortChoice{effort: lowerEffort(e, ultra, l.steps), reason: tag + "tool results only"}
}

// userTurn says why a request keeps the user's effort, or "" when its input
// since the model's last output is only tool results.
func userTurn(input []llm.Item) string {
	if !slices.ContainsFunc(input, modelOutput) {
		return "first request"
	}
	results := false
	for _, it := range slices.Backward(input) {
		if modelOutput(it) {
			break
		}
		if it.Type == llm.ItemToolResult {
			results = true
		} else if m, ok := it.Data.(llm.Message); ok && m.Role == llm.RoleUser {
			return "user message"
		}
	}
	if !results {
		return "no tool results"
	}

	return ""
}

// modelOutput reports whether an item is the model's: its text, a tool
// call, or reasoning.
func modelOutput(it llm.Item) bool {
	if m, ok := it.Data.(llm.Message); ok {
		return m.Role == llm.RoleAssistant
	}

	return it.Type == llm.ItemToolCall || it.Type == llm.ItemReasoning
}

// efforts are the runner's levels, lowest first.
var efforts = []llm.ReasoningEffort{llm.ReasoningEffortLow, llm.ReasoningEffortMedium, llm.ReasoningEffortHigh, llm.ReasoningEffortXHigh, llm.ReasoningEffortMax}

// lowerEffort is the effort steps levels below effort, never below low;
// ultra (the runner's max with the ultra client) is the level above max,
// so one step from it is max and two are xhigh.
func lowerEffort(effort llm.ReasoningEffort, ultra bool, steps int) llm.ReasoningEffort {
	i := slices.Index(efforts, effort)
	if ultra {
		i = len(efforts)
	}
	if i < 0 {
		return effort
	}

	return efforts[max(i-steps, 0)]
}
