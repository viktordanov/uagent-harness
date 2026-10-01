package embedded

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/unreal-agent/harness/llm"

	"github.com/viktordanov/uah/internal/patch"
)

func TestReadExperiments(t *testing.T) {
	x := readExperiments(func(string) string { return " auto-verify,unknown, effort-by-turn" })
	assert.Equal(t, experiments{autoVerify: true, effortByTurn: true}, x)
	assert.Equal(t, experiments{}, readExperiments(func(string) string { return "" }))
}

// TestDetectChecks: one check per project the patch touched, the cheapest
// that compiles: go build (go vet when a test changed), the package's
// typecheck script before build, node --check without a script, and
// py_compile for Python; nothing for other files.
func TestDetectChecks(t *testing.T) {
	ws := t.TempDir()
	write := func(rel, data string) string {
		p := filepath.Join(ws, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(data), 0o644))

		return p
	}
	write("go.mod", "module m\n")
	write("tools/go.mod", "module t\n")
	write("web/package.json", `{"scripts":{"test":"vitest","build":"vite build","typecheck":"tsc --noEmit"}}`)
	write("site/package.json", `{"scripts":{"test":"jest"}}`)
	changes := func(paths ...string) []patch.Change {
		var out []patch.Change
		for _, p := range paths {
			out = append(out, patch.Change{Op: patch.Update, Abs: filepath.Join(ws, p)})
		}

		return out
	}

	assert.Equal(t, []verifyCheck{{dir: ws, command: "go build ./..."}}, detectChecks(ws, changes("a.go", "b/c.go")))
	assert.Equal(t, []verifyCheck{{dir: ws, command: "go vet ./..."}, {dir: filepath.Join(ws, "tools"), command: "go build ./..."}},
		detectChecks(ws, changes("a.go", "a_test.go", "tools/x.go")))
	assert.Equal(t, []verifyCheck{{dir: filepath.Join(ws, "web"), command: "npm run --silent typecheck"}}, detectChecks(ws, changes("web/src/a.ts", "web/src/b.tsx")))
	assert.Equal(t, []verifyCheck{{dir: ws, command: "node --check 'site/a.js'"}}, detectChecks(ws, changes("site/a.js")))
	assert.Empty(t, detectChecks(ws, changes("site/a.ts", "README.md", "notes.txt")))
	assert.Equal(t, []verifyCheck{{dir: ws, command: pyCompile + "'a.py' 'b/c.py'"}}, detectChecks(ws, changes("a.py", "b/c.py")))
	outside := t.TempDir()
	assert.Empty(t, detectChecks(ws, []patch.Change{{Op: patch.Update, Abs: filepath.Join(outside, "x.go")}}), "no module above it in the workspace")
}

func TestContinuation(t *testing.T) {
	msg := func(role llm.Role) llm.Item { return llm.Item{Type: llm.ItemMessage, Data: llm.Message{Role: role, Text: "x"}} }
	call := llm.Item{Type: llm.ItemToolCall, Data: llm.ToolCall{CallID: "c"}}
	result := llm.Item{Type: llm.ItemToolResult, Data: llm.ToolResult{CallID: "c"}}
	reasoning := llm.Item{Type: llm.ItemReasoning}

	assert.False(t, continuation([]llm.Item{msg(llm.RoleSystem), msg(llm.RoleUser)}), "the first request")
	assert.True(t, continuation([]llm.Item{msg(llm.RoleUser), reasoning, call, result}))
	assert.True(t, continuation([]llm.Item{msg(llm.RoleUser), msg(llm.RoleAssistant), call, result, result}))
	assert.False(t, continuation([]llm.Item{msg(llm.RoleUser), call, result, msg(llm.RoleUser)}), "a user message came with the results")
	assert.False(t, continuation([]llm.Item{msg(llm.RoleUser), call, msg(llm.RoleUser), result}), "a steer before the results")
	assert.False(t, continuation([]llm.Item{msg(llm.RoleUser), msg(llm.RoleAssistant)}))
}

func TestLowerEffort(t *testing.T) {
	for in, want := range map[llm.ReasoningEffort]llm.ReasoningEffort{
		llm.ReasoningEffortLow: llm.ReasoningEffortLow, llm.ReasoningEffortMedium: llm.ReasoningEffortLow,
		llm.ReasoningEffortHigh: llm.ReasoningEffortMedium, llm.ReasoningEffortXHigh: llm.ReasoningEffortHigh,
		llm.ReasoningEffortMax: llm.ReasoningEffortXHigh,
	} {
		assert.Equal(t, want, lowerEffort(in, false), in)
	}
	assert.Equal(t, llm.ReasoningEffortMax, lowerEffort(llm.ReasoningEffortMax, true), "ultra")
}
