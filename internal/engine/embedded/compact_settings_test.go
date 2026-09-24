package embedded_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unreallabsai/unreal-agent/harness/llm"

	"github.com/viktordanov/uagent-harness/internal/compaction"
	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/engine/embedded"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

func (e *env) withCompaction(c compaction.Settings, window int64) *embedded.Engine {
	return embedded.New(embedded.Config{StateDir: e.StateDir, Provider: "openai", Getenv: e.getenv, Compaction: c, ContextWindow: window})
}

func TestEmbedded_CompactsWithTheConfiguredModelPromptAndFocus(t *testing.T) {
	e := newEnv(t, fakellm.Reply{Text: "answer one"}, fakellm.Reply{Text: "SUMMARY"}, fakellm.Reply{Text: "answer two"})
	eng := e.withCompaction(compaction.Settings{Model: "gpt-small", Effort: llm.ReasoningEffortLow, Prompt: "CUSTOM PROMPT", UserMessageMaxTokens: 7}, 0)
	s, ev := e.open(t, eng, "")
	ask(t, s, ev, "first message, longer than seven tokens")
	require.NoError(t, s.CompactWith("keep the file names"))
	ask(t, s, ev, "second")

	reqs := e.llm.Requests()
	require.Len(t, reqs, 3)
	summary := reqs[1]
	assert.Equal(t, "gpt-small", summary.Model, "compact_model writes the summary")
	assert.Equal(t, "low", summary.Effort)
	last := summary.UserTexts[len(summary.UserTexts)-1]
	assert.Equal(t, "CUSTOM PROMPT\n\nThe user asked this summary to focus on:\nkeep the file names", last)

	next := reqs[2]
	assert.NotEqual(t, "gpt-small", next.Model, "the session keeps its model")
	require.Len(t, next.UserTexts, 3)
	assert.Contains(t, next.UserTexts[0], "tokens truncated", "the kept message follows compact_user_message_max_tokens")

	rec, _, err := compaction.OpenLog(filepath.Join(e.StateDir, "sessions"), s.ID()).Last()
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "gpt-small", rec.Model)
	assert.Equal(t, 7, rec.Keep, "the record keeps its cap")
	assert.Equal(t, "keep the file names", rec.Focus)
}

func TestEmbedded_TokenLimitCompactsBeforeThePercent(t *testing.T) {
	e := newEnv(t,
		fakellm.Reply{Commands: []string{"echo one"}, InputTokens: 5_000},
		fakellm.Reply{Text: "AUTO"},
		fakellm.Reply{Text: "done", InputTokens: 1_000},
	)
	s, ev := e.open(t, e.withCompaction(compaction.Settings{Percent: 90, TokenLimit: 4_000}, 0), "")
	ask(t, s, ev, "first")

	require.Len(t, e.llm.Requests(), 3)
	assert.Equal(t, 1, countKind[engine.CompactionStarted](ev.all), "5,000 tokens pass the 4,000-token limit, far below 90% of the window")
	u, ok := s.ContextUsage()
	require.True(t, ok)
	assert.Equal(t, u.Window-4_000, u.Buffer, "/context's buffer is the window above the effective limit")
}

// A compaction that leaves the context above the automatic limit (here,
// one long kept message) would otherwise compact again before every request.
func TestEmbedded_AutoCompactionStopsWhenItCannotGetUnderTheLimit(t *testing.T) {
	e := newEnv(t,
		fakellm.Reply{Commands: []string{"echo one"}, InputTokens: 60_000},
		fakellm.Reply{Text: "S1"},
		fakellm.Reply{Commands: []string{"echo two"}, InputTokens: 61_000},
		fakellm.Reply{Commands: []string{"echo three"}, InputTokens: 62_000},
		fakellm.Reply{Text: "done", InputTokens: 63_000},
	)
	eng := e.withCompaction(compaction.Settings{Percent: 50, UserMessageMaxTokens: 100_000}, 100_000)
	s, ev := e.open(t, eng, "")
	ask(t, s, ev, strings.Repeat("x", 220_000)) // 55,000 tokens, kept whole

	assert.Len(t, e.llm.Requests(), 5, "one summary call, not one per request")
	assert.Equal(t, 1, countKind[engine.CompactionStarted](ev.all))
	var done engine.Compacted
	for _, x := range ev.all {
		if v, ok := x.(engine.Compacted); ok {
			done = v
		}
	}
	assert.Empty(t, done.Err)
	assert.Contains(t, done.Warning, "automatic compaction stops for this run")
}
