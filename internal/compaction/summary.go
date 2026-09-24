package compaction

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/unreallabsai/unreal-agent/harness/llm"

	"github.com/viktordanov/uagent-harness/internal/llmcall"
)

// overflowRetries bounds how often a summary call that overflows the window
// is retried with less history.
const overflowRetries = 4

// SummaryRequest is what the summary call sends: the (already compacted)
// history without its system message, then the prompt as a user message.
// The system text is returned separately, for the call's instructions.
func SummaryRequest(view []llm.Item) (system string, input []llm.Item) {
	rest := view
	if len(view) > 0 {
		if m, ok := view[0].Data.(llm.Message); ok && m.Role == llm.RoleSystem {
			system, rest = m.Text, view[1:]
		}
	}
	input = append(input, rest...)
	input = append(input, llm.Item{Type: llm.ItemMessage, Data: llm.Message{Role: llm.RoleUser, Text: Prompt}})

	return system, input
}

// Trim drops the oldest items until the estimate of the rest is at most
// budget tokens, keeping at least the last item, as Codex drops the oldest
// history when a summary call overflows the window. A tool result whose call
// was dropped becomes a user-role note.
func Trim(items []llm.Item, budget int64) []llm.Item {
	total := EstimateTokens(items)
	start := 0
	for start < len(items)-1 && total > budget {
		total -= EstimateTokens(items[start : start+1])
		start++
	}
	if start == 0 {
		return items
	}

	return detachOrphans(items[start:])
}

// Summarizer is a summary call: the history as the model sees it (system
// message first) in, the summary text out. Summarize is the local one; a
// remote compaction endpoint would be another.
type Summarizer func(ctx context.Context, view []llm.Item) (string, error)

// SummaryCall configures Summarize.
type SummaryCall struct {
	Adapter llm.Adapter
	// Model is the model ID; empty lets the adapter pick the live one.
	Model  string
	Effort llm.ReasoningEffort
	// Window is the model's context window in tokens; the history is
	// trimmed to fit it.
	Window   int64
	CacheKey string
}

// Summarize asks the model for a summary of view with Codex's prompt and no
// tools. A history larger than the window is trimmed from the oldest item
// first, and trimmed further when the provider still reports an overflow.
func Summarize(ctx context.Context, call SummaryCall, view []llm.Item) (string, error) {
	system, input := SummaryRequest(view)
	history, prompt := input[:len(input)-1], input[len(input)-1]
	budget := call.Window - EstimateTokens([]llm.Item{prompt}) - EstimateTokens([]llm.Item{llmcall.Message(llm.RoleSystem, system)})
	for attempt := 0; ; attempt++ {
		trimmed := history
		if budget > 0 {
			trimmed = Trim(history, budget)
		}
		res, err := llmcall.Call(ctx, call.Adapter, llmcall.Request{
			Model: call.Model, Effort: call.Effort, Instructions: system,
			Input: append(slices.Clip(trimmed), prompt), CacheKey: call.CacheKey,
		})
		if err == nil {
			return res.Text, nil
		}
		if !errors.Is(err, llmcall.ErrContextWindow) || attempt == overflowRetries || len(trimmed) <= 1 {
			return "", fmt.Errorf("failed to summarize the context: %w", err)
		}
		budget = EstimateTokens(trimmed) * 3 / 4
	}
}
