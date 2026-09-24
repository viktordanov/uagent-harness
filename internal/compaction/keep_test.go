package compaction_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/unreallabsai/unreal-agent/harness/llm"

	"github.com/viktordanov/uagent-harness/internal/compaction"
)

// Codex's own vectors (codex-rs/utils/string/src/truncate/tests.rs).
func TestTruncateMiddle_MatchesCodex(t *testing.T) {
	assert.Equal(t, "short output", compaction.TruncateMiddle("short output", 100))
	assert.Equal(t, "…2 tokens truncated…", compaction.TruncateMiddle("abcdef", 0))
	assert.Equal(t, "😀😀😀😀…8 tokens truncated… line with text\n",
		compaction.TruncateMiddle("😀😀😀😀😀😀😀😀😀😀\nsecond line with text\n", 8))
	assert.Empty(t, compaction.TruncateMiddle("", 0))
}

func TestKept_NewestUserMessagesUpToTheCap(t *testing.T) {
	covered := []llm.Item{
		msg(llm.RoleUser, "oldest, dropped"), call("a"), result("a", "x"),
		msg(llm.RoleUser, strings.Repeat("m", 40)), // 10 tokens: crosses the cap, shortened
		msg(llm.RoleUser, "Heartbeat: waited 600 seconds for tool calls.\nRunning: []"),
		msg(llm.RoleAssistant, "ok"),
		msg(llm.RoleUser, strings.Repeat("n", 24)), // 6 tokens
		msg(llm.RoleUser, "last"),                  // 1 token
	}
	got := texts(compaction.Kept(covered, 10))
	require.Len(t, got, 3, "the heartbeat, the assistant, the tool items, and the oldest message are left out")
	assert.Equal(t, "user: "+strings.Repeat("m", 6)+"…7 tokens truncated…"+strings.Repeat("m", 6), got[0])
	assert.Equal(t, []string{"user: " + strings.Repeat("n", 24), "user: last"}, got[1:])

	all := texts(compaction.Kept(covered, compaction.UserMessageMaxTokens))
	assert.Equal(t, []string{
		"user: oldest, dropped", "user: " + strings.Repeat("m", 40), "user: " + strings.Repeat("n", 24), "user: last",
	}, all, "under the cap every user message stays verbatim and in order")
}

func TestApply_CapsKeptMessagesAndKeepsTheTailWhole(t *testing.T) {
	long := strings.Repeat("x", 4*compaction.UserMessageMaxTokens+400) // 100 tokens over
	history := []llm.Item{msg(llm.RoleSystem, "sys"), msg(llm.RoleUser, "first"), msg(llm.RoleUser, long), call("a"), result("a", "y")}
	rec, err := compaction.NewRecord(history, "S", compaction.TriggerAuto, "m", time.Now())
	require.NoError(t, err)
	got, err := compaction.Apply(append(history, msg(llm.RoleUser, long)), rec)
	require.NoError(t, err)
	out := texts(got)
	require.Len(t, out, 4, "the older message is past the cap")
	assert.Contains(t, out[1], "…100 tokens truncated…")
	assert.Equal(t, "user: "+compaction.SummaryPrefix+"\nS", out[2])
	assert.Equal(t, "user: "+long, out[3], "new input after the summary is never cut")
}

func TestApply_SummaryOfASummary(t *testing.T) {
	history := []llm.Item{msg(llm.RoleSystem, "sys"), msg(llm.RoleUser, "one"), call("a"), result("a", "x")}
	first, err := compaction.NewRecord(history, "S1", compaction.TriggerManual, "m", time.Now())
	require.NoError(t, err)
	history = append(history, msg(llm.RoleUser, "two"), call("b"), result("b", "y"))
	view, err := compaction.Apply(history, first)
	require.NoError(t, err)
	_, input := compaction.SummaryRequest(view, "")
	assert.Contains(t, texts(input), "user: "+compaction.SummaryPrefix+"\nS1", "the second summary call sees the first summary")

	second, err := compaction.NewRecord(history, "S2", compaction.TriggerManual, "m", time.Now())
	require.NoError(t, err)
	got, err := compaction.Apply(append(history, msg(llm.RoleUser, "three")), second)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"system: sys", "user: one", "user: two", "user: " + compaction.SummaryPrefix + "\nS2", "user: three",
	}, texts(got), "the second summary replaces the first; user messages stay")
}

func TestApply_ALateToolOutputBecomesANote(t *testing.T) {
	image := llm.Item{Type: llm.ItemToolResult, Data: llm.ToolResult{CallID: "a", Output: []llm.ToolResultOutput{
		{Kind: llm.ToolResultText, Value: "done"}, {Kind: llm.ToolResultImage, Value: "data:image/png;base64,AAAA"},
	}}}
	history := []llm.Item{msg(llm.RoleSystem, "sys"), msg(llm.RoleUser, "go"), call("a"), result("a", "running")}
	rec, err := compaction.NewRecord(history, "S", compaction.TriggerAuto, "m", time.Now())
	require.NoError(t, err)
	got, err := compaction.Apply(append(history, image), rec)
	require.NoError(t, err)
	assert.Equal(t, "user: Output of the earlier tool call a, which the summary covers:\ndone\n[image omitted]", texts(got)[3])
}
