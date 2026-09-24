package compaction

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/unreallabsai/unreal-agent/harness/llm"
)

// UserMessageMaxTokens caps the user messages a compaction keeps, as Codex
// does (COMPACT_USER_MESSAGE_MAX_TOKENS): the newest messages are kept whole
// until the cap, the one that crosses it is shortened in the middle, and
// older ones are left to the summary.
const UserMessageMaxTokens = 20_000

// heartbeatPrefix starts the user-role message the runner (v0.1.1,
// coordinator.postHeartbeat) adds while it waits for tool calls. It is not
// the user's, so a compaction leaves it to the summary, as Codex keeps only
// real user messages.
const heartbeatPrefix = "Heartbeat: waited "

// bytesPerToken is Codex's estimate (codex-rs/utils/string, APPROX_BYTES_PER_TOKEN).
const bytesPerToken = 4

// IsHeartbeat reports whether the item is the runner's heartbeat message.
func IsHeartbeat(item llm.Item) bool {
	m, ok := item.Data.(llm.Message)

	return ok && IsUserMessage(item) && strings.HasPrefix(m.Text, heartbeatPrefix)
}

// Kept is the user messages among covered that a compaction keeps, in
// order: heartbeats are left out, and the newest messages are kept up to
// maxTokens, Codex's build_compacted_history_with_limit.
func Kept(covered []llm.Item, maxTokens int) []llm.Item {
	var picked []llm.Item
	remaining := maxTokens
	for i := len(covered) - 1; i >= 0 && remaining > 0; i-- {
		item := covered[i]
		if !IsUserMessage(item) || IsHeartbeat(item) {
			continue
		}
		m, _ := item.Data.(llm.Message)
		tokens := ApproxTokens(m.Text)
		if tokens > remaining {
			m.Text = TruncateMiddle(m.Text, remaining)
			item.Data = m
			remaining = 0
		} else {
			remaining -= tokens
		}
		picked = append(picked, item)
	}
	slices.Reverse(picked)

	return picked
}

// ApproxTokens is Codex's estimate of text's tokens: bytes/4, rounded up.
func ApproxTokens(text string) int {
	return (len(text) + bytesPerToken - 1) / bytesPerToken
}

// TruncateMiddle shortens text to about maxTokens, keeping its beginning and
// end on UTF-8 boundaries with Codex's marker between them ("…N tokens
// truncated…"), as codex-rs truncate_middle_with_token_budget does.
func TruncateMiddle(text string, maxTokens int) string {
	maxBytes := maxTokens * bytesPerToken
	if text == "" || (maxTokens > 0 && len(text) <= maxBytes) {
		return text
	}
	if maxBytes <= 0 {
		return fmt.Sprintf("…%d tokens truncated…", ApproxTokens(text))
	}
	marker := fmt.Sprintf("…%d tokens truncated…", ApproxTokens(text[maxBytes:]))
	left := maxBytes / 2
	right := maxBytes - left
	prefixEnd := 0
	for prefixEnd < len(text) {
		_, size := utf8.DecodeRuneInString(text[prefixEnd:])
		if prefixEnd+size > left {
			break
		}
		prefixEnd += size
	}
	suffixStart := len(text) - right
	for suffixStart < len(text) && !utf8.RuneStart(text[suffixStart]) {
		suffixStart++
	}
	suffixStart = max(suffixStart, prefixEnd)

	return text[:prefixEnd] + marker + text[suffixStart:]
}
