package compaction

import (
	"encoding/json/v2"

	"github.com/unreallabsai/unreal-agent/harness/llm"
)

// imageBytes is Codex's estimate for one image (RESIZED_IMAGE_BYTES_ESTIMATE
// in codex-rs/core/src/context_manager/history.rs), about 1,844 tokens.
const imageBytes = 7373

// InUse is the context a request uses, as Codex counts it
// (ContextManager::get_total_token_usage): the last response's total tokens
// plus an estimate of the items added after the last item the model
// produced. When the last response reported no usage (lastTotal <= 0: none
// yet, a provider without usage, or right after a compaction), the whole
// request is estimated.
func InUse(view []llm.Item, lastTotal int64) int64 {
	if lastTotal <= 0 {
		return EstimateTokens(view)
	}
	start := len(view)
	for start > 0 && !modelGenerated(view[start-1]) {
		start--
	}

	return lastTotal + EstimateTokens(view[start:])
}

// EstimateTokens is Codex's estimate of the tokens items take in a request:
// the model-visible bytes divided by four, rounded up.
func EstimateTokens(items []llm.Item) int64 {
	var total int64
	for _, item := range items {
		total += (itemBytes(item) + bytesPerToken - 1) / bytesPerToken
	}

	return total
}

func modelGenerated(item llm.Item) bool {
	switch d := item.Data.(type) {
	case llm.Message:
		return d.Role == llm.RoleAssistant
	case llm.ToolCall, llm.Reasoning:
		return true
	}

	return false
}

// itemBytes is the model-visible size of one item, as Codex's
// estimate_response_item_model_visible_bytes counts it.
func itemBytes(item llm.Item) int64 {
	switch d := item.Data.(type) {
	case llm.Message:
		return int64(len(d.Text))
	case llm.ToolCall:
		return int64(len(d.Name) + len(d.Arguments))
	case llm.ToolResult:
		n := int64(len(d.CallID))
		for _, o := range d.Output {
			if o.Kind == llm.ToolResultImage {
				n += imageBytes
			} else {
				n += int64(len(o.Value))
			}
		}

		return n
	case llm.Reasoning:
		return reasoningBytes(d)
	}

	return 0
}

// reasoningBytes counts encrypted reasoning as Codex does (3/4 of the
// encoded length, less 650); plaintext reasoning is not replayed.
func reasoningBytes(r llm.Reasoning) int64 {
	var raw struct {
		EncryptedContent string `json:"encrypted_content"`
	}
	if len(r.Raw) == 0 || json.Unmarshal(r.Raw, &raw) != nil {
		return 0
	}

	return max(int64(len(raw.EncryptedContent))*3/4-650, 0)
}
