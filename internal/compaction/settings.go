package compaction

import (
	"strings"

	"github.com/unreallabsai/unreal-agent/harness/llm"
)

// Settings are the configurable parts of compaction. The zero value turns
// automatic compaction off and otherwise takes Codex's defaults: the
// session's model and effort, Codex's prompt, and the 20,000-token cap on
// kept user messages.
type Settings struct {
	// Percent is auto_compact_percent: automatic compaction starts when the
	// context in use reaches this share of the window (0: never).
	Percent int
	// TokenLimit is Codex's model_auto_compact_token_limit: when positive,
	// automatic compaction starts at this many tokens if that comes before
	// Percent of the window, as Codex takes the lower of the two.
	TokenLimit int64
	// Model and Effort make the summary call ("": the session's), as
	// compact_model and compact_effort.
	Model  string
	Effort llm.ReasoningEffort
	// Prompt replaces Codex's summary prompt (compact_prompt, or the text of
	// experimental_compact_prompt_file) when not empty.
	Prompt string
	// UserMessageMaxTokens caps the user messages a compaction keeps
	// verbatim (compact_user_message_max_tokens); 0 is Codex's 20,000, at
	// most a quarter of the window (KeepFor).
	UserMessageMaxTokens int
}

// Limit is the tokens in use at which automatic compaction starts for a
// window; 0 means never. Percent 0 turns it off; a token limit only lowers
// it, as Codex's auto_compact_token_limit takes the minimum with 90% of the
// window.
func (s Settings) Limit(window int64) int64 {
	limit := AutoLimit(window, s.Percent)
	if limit == 0 || s.TokenLimit <= 0 {
		return limit
	}

	return min(limit, s.TokenLimit)
}

// SummaryPrompt is the prompt the summary call ends with: the configured
// one or Codex's, and the user's focus for this compaction when given, as
// Claude Code's /compact [instructions].
func (s Settings) SummaryPrompt(focus string) string {
	prompt := Prompt
	if p := strings.TrimSpace(s.Prompt); p != "" {
		prompt = p
	}
	if focus = strings.TrimSpace(focus); focus != "" {
		prompt += "\n\nThe user asked this summary to focus on:\n" + focus
	}

	return prompt
}

// KeepTokens is the configured cap on kept user messages, or Codex's.
func (s Settings) KeepTokens() int {
	if s.UserMessageMaxTokens > 0 {
		return s.UserMessageMaxTokens
	}

	return UserMessageMaxTokens
}

// KeepFor is the cap a compaction uses in a window: the configured one, or
// Codex's 20,000 tokens but at most a quarter of the window, so a small
// model's compacted context is not mostly old messages (Codex's windows are
// 272,000 tokens, where 20,000 is a fourteenth).
func (s Settings) KeepFor(window int64) int {
	if s.UserMessageMaxTokens > 0 || window <= 0 {
		return s.KeepTokens()
	}

	return int(max(min(int64(UserMessageMaxTokens), window/4), 1))
}
