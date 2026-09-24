package compaction_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/unreallabsai/unreal-agent/harness/llm"

	"github.com/viktordanov/uagent-harness/internal/compaction"
)

func msg(role llm.Role, text string) llm.Item {
	return llm.Item{Type: llm.ItemMessage, Data: llm.Message{Role: role, Text: text}}
}

func call(id string) llm.Item {
	return llm.Item{Type: llm.ItemToolCall, Data: llm.ToolCall{CallID: id, Name: "Bash", Arguments: `{}`}}
}

func result(id, text string) llm.Item {
	return llm.Item{Type: llm.ItemToolResult, Data: llm.ToolResult{CallID: id, Output: []llm.ToolResultOutput{{Kind: llm.ToolResultText, Value: text}}}}
}

func texts(items []llm.Item) []string {
	var out []string
	for _, it := range items {
		switch d := it.Data.(type) {
		case llm.Message:
			out = append(out, string(d.Role)+": "+d.Text)
		case llm.ToolCall:
			out = append(out, "call "+d.CallID)
		case llm.ToolResult:
			out = append(out, "result "+d.CallID)
		}
	}

	return out
}

func TestApply_KeepsUserMessagesAndReplacesTheRest(t *testing.T) {
	history := []llm.Item{
		msg(llm.RoleSystem, "sys"), msg(llm.RoleUser, "first"), call("a"), msg(llm.RoleAssistant, "working"),
		result("a", "out a"), msg(llm.RoleUser, "second"), call("b"),
	}
	rec, err := compaction.NewRecord(history, "the summary", compaction.TriggerManual, "m", time.Now())
	require.NoError(t, err)
	assert.Equal(t, 6, rec.Covered)

	later := append(history, result("b", "out b"), call("c"), result("c", "out c"), msg(llm.RoleUser, "third"))
	got, err := compaction.Apply(later, rec)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"system: sys", "user: first", "user: second", "user: " + compaction.SummaryPrefix + "\nthe summary",
		"user: Output of the earlier tool call b, which the summary covers:\nout b",
		"call c", "result c", "user: third",
	}, texts(got))
}

func TestApply_RejectsAnotherHistory(t *testing.T) {
	history := []llm.Item{msg(llm.RoleSystem, "sys"), msg(llm.RoleUser, "first"), call("a")}
	rec, err := compaction.NewRecord(history, "s", compaction.TriggerAuto, "m", time.Now())
	require.NoError(t, err)

	_, err = compaction.Apply([]llm.Item{msg(llm.RoleSystem, "sys"), msg(llm.RoleUser, "other"), call("a")}, rec)
	require.ErrorIs(t, err, compaction.ErrMismatch)
	_, err = compaction.Apply(history[:2], rec)
	require.ErrorIs(t, err, compaction.ErrMismatch)
	// The system message may change (instructions, skills) without breaking it.
	_, err = compaction.Apply(append([]llm.Item{msg(llm.RoleSystem, "new")}, history[1:]...), rec)
	require.NoError(t, err)
}

func TestSummaryRequest(t *testing.T) {
	system, input := compaction.SummaryRequest([]llm.Item{msg(llm.RoleSystem, "sys"), msg(llm.RoleUser, "hi")})
	assert.Equal(t, "sys", system)
	assert.Equal(t, []string{"user: hi", "user: " + compaction.Prompt}, texts(input))
}

func TestWindowAndMeter(t *testing.T) {
	assert.Equal(t, int64(272_000), compaction.ContextWindow("unknown", 0))
	assert.Equal(t, int64(372_000), compaction.ContextWindow("gpt-daybreak-red-latest", 0))
	assert.Equal(t, int64(1000), compaction.ContextWindow("gpt-6-sol", 1000))
	assert.Equal(t, int64(244_800), compaction.AutoLimit(272_000, 90))
	assert.Equal(t, int64(0), compaction.AutoLimit(272_000, 0))
	assert.Equal(t, 100, compaction.PercentLeft(5_000, 272_000))
	assert.Equal(t, 50, compaction.PercentLeft(142_000, 272_000))
	assert.Equal(t, 0, compaction.PercentLeft(400_000, 272_000))
}
