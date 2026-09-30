package eval_test

import (
	"encoding/json/v2"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/unreallabsai/unreal-agent/harness/llm"

	"github.com/viktordanov/uah/internal/compaction"
	"github.com/viktordanov/uah/internal/compaction/eval"
)

func msg(role llm.Role, text string) llm.Item {
	return llm.Item{Type: llm.ItemMessage, Data: llm.Message{Role: role, Text: text}}
}

func bash(id, command, output string) []llm.Item {
	args, _ := json.Marshal(map[string]string{"command": command})

	return []llm.Item{
		{Type: llm.ItemToolCall, Data: llm.ToolCall{CallID: id, Name: "Bash", Arguments: string(args)}},
		{Type: llm.ItemToolResult, Data: llm.ToolResult{CallID: id, Output: []llm.ToolResultOutput{{Kind: llm.ToolResultText, Value: output}}}},
	}
}

func request() []llm.Item {
	out := []llm.Item{msg(llm.RoleSystem, "sys"), msg(llm.RoleUser, "fix it")}
	out = append(out, bash("c1", "cat a/x.go", strings.Repeat("x", 800))...)
	out = append(out, bash("c2", "go test ./...", "Stderr:\n--- FAIL: TestX\nExit code: 1")...)

	return append(out, msg(llm.RoleUser, "next"))
}

func TestMeasure_TheFullHistoryKeepsEverything(t *testing.T) {
	c := eval.Case{Before: request(), Window: 1000, Later: []llm.ToolCall{{Name: "Bash", Arguments: `{"command":"cat a/x.go"}`}}}
	r := eval.Measure(c, c.Before)
	assert.Equal(t, r.Before, r.After)
	assert.Zero(t, r.CacheMiss)
	assert.Equal(t, 1000-r.After, r.Headroom)
	assert.Equal(t, eval.Count{Found: 1, Total: 1}, r.Recall[eval.KindRead])
	assert.Equal(t, eval.Count{Found: 1, Total: 1}, r.Recall[eval.KindTail])
	assert.Equal(t, eval.Count{Found: 0, Total: 1}, r.Refetch, "a later read of a kept output costs nothing")
}

func TestMeasure_ASummaryLosesWhatItDoesNotSay(t *testing.T) {
	c := eval.Case{Before: request(), Later: []llm.ToolCall{
		{Name: "Bash", Arguments: `{"command":"cat a/x.go"}`},
		{Name: "Bash", Arguments: `{"command":"ls"}`},
	}}
	rec, err := compaction.NewRecord(c.Before, "Tests fail.", compaction.TriggerManual, "", fixed)
	assert.NoError(t, err)
	after, err := compaction.Apply(c.Before, rec)
	assert.NoError(t, err)
	r := eval.Measure(c, after)
	assert.Less(t, r.After, r.Before)
	assert.Equal(t, eval.Count{Found: 0, Total: 1}, r.Recall[eval.KindRead])
	assert.Equal(t, eval.Count{Found: 0, Total: 1}, r.Recall[eval.KindFailing])
	assert.Equal(t, eval.Count{Found: 2, Total: 2}, r.UserKept, "the user's message stays")
	assert.Equal(t, eval.Count{Found: 1, Total: 2}, r.Refetch, "cat a/x.go reads again what the summary dropped")
	assert.Positive(t, r.CacheMiss)
	assert.Positive(t, r.ByActor[eval.ActorSummary])

	rec.Ledger = compaction.Ledger(c.Before[1:1+rec.Covered], "")
	after, _ = compaction.Apply(c.Before, rec)
	r = eval.Measure(c, after)
	assert.Equal(t, eval.Count{Found: 1, Total: 1}, r.Recall[eval.KindRead], "the ledger names it")
	assert.Equal(t, eval.Count{Found: 1, Total: 1}, r.Recall[eval.KindTail])
}

func TestSummarize_MediansAndPools(t *testing.T) {
	a := eval.Summarize("s", false, []eval.Result{
		{Before: 100, After: 10, Refetch: eval.Count{Found: 1, Total: 4}},
		{Before: 100, After: 30, Refetch: eval.Count{Found: 1, Total: 4}},
		{Before: 100, After: 20},
	})
	assert.InDelta(t, 20.0, a.After, 0.001)
	assert.InDelta(t, 0.2, a.Ratio, 0.001)
	assert.Equal(t, eval.Count{Found: 2, Total: 8}, a.Refetch)
}

var fixed = time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
