package compaction_test

import (
	"encoding/json/v2"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/viktordanov/unreal-agent/harness/llm"

	"github.com/viktordanov/uah/internal/compaction"
)

// toolCall is a call to a tool with arguments given as a map.
func toolCall(id, name string, args map[string]any) llm.Item {
	b, _ := json.Marshal(args)

	return llm.Item{Type: llm.ItemToolCall, Data: llm.ToolCall{CallID: id, Name: name, Arguments: string(b)}}
}

func bash(id, command, output string) []llm.Item {
	return []llm.Item{toolCall(id, "Bash", map[string]any{"command": command}), result(id, output)}
}

func applyPatch(id, text, output string) []llm.Item {
	return []llm.Item{toolCall(id, "apply_patch", map[string]any{"input": text}), result(id, output)}
}

func history(parts ...[]llm.Item) []llm.Item {
	var out []llm.Item
	for _, p := range parts {
		out = append(out, p...)
	}

	return out
}

const updatePatch = "*** Begin Patch\n*** Update File: internal/a.go\n@@ func A()\n keep\n-old one\n-old two\n+new one\n*** Add File: docs/b.md\n+line 1\n+line 2\n*** Delete File: gone.txt\n*** End Patch"

func TestExtractFacts(t *testing.T) {
	items := history(
		bash("c1", "cat internal/a.go docs/readme.md", "package a"),
		bash("c2", "go test ./...", "--- FAIL: TestA\nStderr:\nexit status 1\npanic: boom\nExit code: 1"),
		bash("c3", "go vet ./...", "Stderr:\nvet: bad\nExit code: 2"),
		bash("c2b", "go vet ./...", "ok"),
		applyPatch("c4", updatePatch, "Success. Updated the following files:\nM internal/a.go"),
		applyPatch("c5", "*** Begin Patch\n*** Add File: never.go\n+x\n*** End Patch", "apply_patch verification failed: exists"),
		[]llm.Item{toolCall("c6", "SkillUse", map[string]any{"name": "go-testing-patterns"}), result("c6", "# Skill body")},
		[]llm.Item{toolCall("c7", "spawn_agent", map[string]any{"message": "x"}), result("c7", `{"agent_id":"subagent-1","nickname":"Ada"}`)},
		[]llm.Item{toolCall("c8", "spawn_agent", map[string]any{"message": "y"}), result("c8", `{"agent_id":"subagent-2","nickname":"Bo"}`)},
		[]llm.Item{toolCall("c9", "close_agent", map[string]any{"target": "subagent-2"}), result("c9", `{"previous_status":"completed"}`)},
		[]llm.Item{toolCall("c10", "ViewImage", map[string]any{"path": "shots/ui.png"}), result("c10", "")},
	)
	f := compaction.ExtractFacts(items)
	assert.Equal(t, []compaction.FileChange{
		{Path: "internal/a.go", Added: 1, Removed: 2},
		{Path: "docs/b.md", Added: 2, Created: true},
		{Path: "gone.txt", Deleted: true},
	}, f.Changed, "only patches that applied count; context lines are not changes")
	require.Len(t, f.Failing, 1, "go vet passed on its last run")
	assert.Equal(t, compaction.FailedCommand{Command: "go test ./...", Exit: 1, Tail: "exit status 1\npanic: boom"}, f.Failing[0])
	assert.Equal(t, []string{"docs/readme.md", "shots/ui.png"}, f.Read, "changed files are listed once, as changed")
	assert.Equal(t, []string{"go-testing-patterns"}, f.Skills)
	assert.Equal(t, []string{"subagent-1 (Ada)"}, f.Agents)
}

// TestExtractFacts_FreeformPatch: a freeform apply_patch call's input is
// the raw patch, and its changes count as the function form's do.
func TestExtractFacts_FreeformPatch(t *testing.T) {
	call := llm.Item{Type: llm.ItemToolCall, Data: llm.ToolCall{CallID: "c1", Name: "apply_patch", Arguments: updatePatch + "\n", Custom: true}}
	f := compaction.ExtractFacts([]llm.Item{call, result("c1", "Success. Updated the following files:\nM internal/a.go")})
	assert.Equal(t, []compaction.FileChange{
		{Path: "internal/a.go", Added: 1, Removed: 2},
		{Path: "docs/b.md", Added: 2, Created: true},
		{Path: "gone.txt", Deleted: true},
	}, f.Changed)
}

func TestPathsIn(t *testing.T) {
	assert.Equal(t, []string{"a.go", "b/c.go", "main.rs"}, compaction.PathsIn("cat a.go b/c.go main.rs"))
	assert.Equal(t, []string{"internal/x/y.go"}, compaction.PathsIn(`sed -n '1,40p' internal/x/y.go`))
	assert.Empty(t, compaction.PathsIn("ls -la"))
}

func TestLedger(t *testing.T) {
	items := history(
		bash("c1", "cat internal/a.go docs/readme.md", "package a"),
		bash("c2", "go test ./...", "Stderr:\npanic: boom\nExit code: 1"),
		applyPatch("c4", updatePatch, "Success. Updated the following files:\nM internal/a.go"),
		[]llm.Item{toolCall("c6", "SkillUse", map[string]any{"name": "go-testing-patterns"}), result("c6", "# Skill")},
	)
	got := compaction.Ledger(items, "keep the failing test names")
	assert.Equal(t, "<uah_state_ledger>\nFacts uah read from the tool calls this summary replaces:\n"+
		"Changed files (apply_patch): internal/a.go (+1 −2); docs/b.md (new, +2); gone.txt (deleted)\n"+
		"Failing commands (their last run failed): `go test ./...` exit 1: panic: boom\n"+
		"Files read: docs/readme.md\n"+
		"Skills loaded (SkillUse again for the body): go-testing-patterns\n"+
		"Compaction focus: keep the failing test names\n"+
		"</uah_state_ledger>", got)
	assert.Empty(t, compaction.Ledger(history(bash("c1", "echo hi", "hi")), ""), "no facts, no ledger")
}

func TestLedger_StaysUnderItsCap(t *testing.T) {
	var parts [][]llm.Item
	for i := range 400 {
		parts = append(parts,
			bash(fmt.Sprintf("r%d", i), fmt.Sprintf("cat pkg%d/file%d.go", i, i), "x"),
			bash(fmt.Sprintf("f%d", i), fmt.Sprintf("go test ./pkg%d/...", i), "Stderr:\n"+strings.Repeat("e", 900)+"\nExit code: 1"),
			applyPatch(fmt.Sprintf("p%d", i), fmt.Sprintf("*** Begin Patch\n*** Add File: new%d/%s.go\n+x\n*** End Patch", i, strings.Repeat("n", 50)), "Success."),
		)
	}
	got := compaction.Ledger(history(parts...), strings.Repeat("focus ", 100))
	assert.LessOrEqual(t, compaction.ApproxTokens(got), compaction.LedgerMaxTokens+30, "the cap and the frame")
	assert.Contains(t, got, "file399.go", "the newest entries stay")
	assert.Contains(t, got, "earlier not listed")
}

func TestApply_SendsTheLedgerAfterTheSummary(t *testing.T) {
	input := []llm.Item{msg(llm.RoleSystem, "sys"), msg(llm.RoleUser, "fix it"), call("c1"), result("c1", "out"), msg(llm.RoleUser, "next")}
	rec, err := compaction.NewRecord(input, "SUMMARY", compaction.TriggerManual, "", time.Time{})
	require.NoError(t, err)
	rec.Ledger = "<uah_state_ledger>\nx\n</uah_state_ledger>"
	out, err := compaction.Apply(input, rec)
	require.NoError(t, err)
	assert.Equal(t, []string{"system: sys", "user: fix it", "user: " + compaction.SummaryPrefix + "\nSUMMARY\n\n<uah_state_ledger>\nx\n</uah_state_ledger>", "user: next"}, texts(out))
}
