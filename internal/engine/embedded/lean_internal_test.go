package embedded

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/unreal-agent/harness/llm"
)

// leanInput builds a request's input: a system and a user message, then
// each step's model call and its result.
type leanInput struct {
	items []llm.Item
	n     int
}

func newLeanInput(task string) *leanInput {
	return &leanInput{items: []llm.Item{msg(llm.RoleSystem, "sys"), msg(llm.RoleUser, task)}}
}

func msg(role llm.Role, text string) llm.Item {
	return llm.Item{Type: llm.ItemMessage, Data: llm.Message{Role: role, Text: text}}
}

// turn adds one model output with a call per result.
func (in *leanInput) turn(results ...leanResult) *leanInput {
	in.items = append(in.items, llm.Item{Type: llm.ItemReasoning, Data: llm.Reasoning{}})
	var outs []llm.Item
	for _, r := range results {
		in.n++
		id := fmt.Sprintf("c%d", in.n)
		in.items = append(in.items, llm.Item{Type: llm.ItemToolCall, Data: llm.ToolCall{CallID: id, Name: r.name, Arguments: r.args, Custom: r.name == "apply_patch"}})
		outs = append(outs, llm.Item{Type: llm.ItemToolResult, Data: llm.ToolResult{CallID: id, Output: []llm.ToolResultOutput{{Kind: llm.ToolResultText, Value: r.out}}}})
	}
	in.items = append(in.items, outs...)

	return in
}

func (in *leanInput) user(text string) *leanInput {
	in.items = append(in.items, msg(llm.RoleUser, text))

	return in
}

type leanResult struct{ name, args, out string }

func shell(command, out string) leanResult {
	args, _ := json.Marshal(map[string]string{"command": command})

	return leanResult{name: "Bash", args: string(args), out: out}
}

var (
	patched     = leanResult{name: "apply_patch", args: "*** Begin Patch\n*** End Patch\n", out: "Success. Updated the following files:\nM a.go\n"}
	patchFailed = leanResult{name: "apply_patch", args: "*** Begin Patch\n*** End Patch\n", out: "apply_patch verification failed: no such file"}
	testsPassed = shell("rtk go test ./...", "ok  \texample.com/m\t0.2s\n"+strings.Repeat("=== RUN x\n", 40))
	testsFailed = shell("go test ./...", "--- FAIL: TestX\nFAIL\nExit code: 1")
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name   string
		result leanResult
		kind   resultKind
		signal string
	}{
		{"a patch that applied", patched, resultConfirm, "patch applied"},
		{"a patch that failed", patchFailed, resultFailure, "patch failed"},
		{"tests that passed, however long", testsPassed, resultConfirm, "test or build passed"},
		{"tests that failed", testsFailed, resultFailure, "exit 1"},
		{"a build through rtk proxy sh -c", shell(`rtk proxy sh -c 'go build ./... && go vet ./...'`, ""), resultConfirm, "test or build passed"},
		{"a short command", shell("mkdir -p out && touch out/x", "(no output)"), resultConfirm, "short output"},
		{"a long output", shell("./run.sh", strings.Repeat("line\n", 31)), resultContent, "long output (155 bytes)"},
		{"a read", shell("cat main.go", "package main\n"), resultContent, "read"},
		{"a ranged read", shell("sed -n 1,40p main.go", "package main\n"), resultContent, "read"},
		{"a listing", shell("ls -la", "a\nb\n"), resultContent, "listing"},
		{"a search", shell("rg -n Record .", "a.go:1:Record\n"), resultContent, "search"},
		{"a dump", shell("git diff", "diff --git a/x b/x\n"), resultContent, "dump"},
		{"a test and a dump", shell("go test ./... && cat out.txt", "ok\n"), resultContent, "dump"},
		{"a refusal", shell("curl x", "not run: the user declined this command."), resultFailure, "refused"},
		{"a command that did not run", shell("x", "Error: shell operation failed"), resultFailure, "did not run"},
		{"a call still running", shell("sleep 600", "Still running after 5 minutes\npartial"), resultContent, "still running"},
		{"MCP", leanResult{name: "mcp__docs__search", args: "{}", out: "ok"}, resultContent, "mcp"},
		{"a skill", leanResult{name: "SkillUse", args: "{}", out: "skill"}, resultContent, "SkillUse"},
		{"a subagent", leanResult{name: "wait_agent", args: "{}", out: "done"}, resultContent, "wait_agent"},
		{"an image", leanResult{name: "ViewImage", args: "{}", out: "img"}, resultContent, "ViewImage"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := readHistory(newLeanInput("do it").turn(tt.result).items)
			c := h.turns[0].results[0]
			assert.Equal(t, tt.kind, c.kind)
			assert.Equal(t, tt.signal, c.signal)
		})
	}
	h := readHistory(newLeanInput("x").turn(shell("rtk proxy sh -c 'go  test ./...'", "FAIL\nExit code: 2")).items)
	assert.Equal(t, "go test ./...", h.turns[0].results[0].command, "a failed command, without its wrappers and with single spaces")
}

// routeAt is the effort the rule picks at high for the input.
func routeAt(rule leanRule, in *leanInput, top llm.ReasoningEffort) effortChoice {
	l := &leanRouter{rule: rule, top: func(string) llm.ReasoningEffort { return top }}

	return l.route(in.items, llm.ReasoningEffortHigh, false, "m")
}

func TestLeanRules_KeepTheEffortForTheUser(t *testing.T) {
	for _, rule := range []leanRule{leanR0, leanR1, leanR2, leanR3} {
		c := routeAt(rule, newLeanInput("fix it"), "")
		assert.Equal(t, llm.ReasoningEffortHigh, c.effort, "%s: the first request", rule)
		assert.Equal(t, rule.String()+": first request", c.reason)
		c = routeAt(rule, newLeanInput("fix it").turn(patched).user("also this"), "")
		assert.Equal(t, llm.ReasoningEffortHigh, c.effort, "%s: a user message came with the results", rule)
		assert.Equal(t, rule.String()+": user message", c.reason)
	}
}

func TestLeanRules(t *testing.T) {
	read := shell("cat a.go", "package a\n")
	tests := []struct {
		name  string
		in    *leanInput
		want  map[leanRule]llm.ReasoningEffort
		since string // the reason under r1
	}{
		{
			name: "confirmations only", in: newLeanInput("fix it").turn(patched, testsPassed),
			want:  map[leanRule]llm.ReasoningEffort{leanR0: "medium", leanR1: "medium", leanR2: "medium", leanR3: "medium"},
			since: "r1: confirmations: patch applied, test or build passed",
		},
		{
			name: "a read keeps the effort, but not under r0", in: newLeanInput("fix it").turn(patched, read),
			want:  map[leanRule]llm.ReasoningEffort{leanR0: "medium", leanR1: "high", leanR2: "high", leanR3: "high"},
			since: "r1: read",
		},
		{
			name: "a failure keeps the effort", in: newLeanInput("fix it").turn(testsFailed),
			want:  map[leanRule]llm.ReasoningEffort{leanR0: "medium", leanR1: "high", leanR2: "high", leanR3: "high"},
			since: "r1: exit 1",
		},
		{
			name: "the same command failed twice: r2 and r3 think harder", in: newLeanInput("fix it").turn(testsFailed).turn(testsFailed),
			want:  map[leanRule]llm.ReasoningEffort{leanR0: "medium", leanR1: "high", leanR2: "xhigh", leanR3: "xhigh"},
			since: "r1: exit 1",
		},
		{
			name: "different commands failed", in: newLeanInput("fix it").turn(testsFailed).turn(shell("go vet ./...", "Exit code: 1")),
			want:  map[leanRule]llm.ReasoningEffort{leanR0: "medium", leanR1: "high", leanR2: "high", leanR3: "high"},
			since: "r1: exit 1",
		},
		{
			name: "a reading task: r3 never goes lower", in: newLeanInput("Review the branch and report why it fails").turn(shell("mkdir -p x", "")),
			want:  map[leanRule]llm.ReasoningEffort{leanR0: "medium", leanR1: "medium", leanR2: "medium", leanR3: "high"},
			since: "r1: confirmations: short output",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for rule, want := range tt.want {
				assert.Equal(t, want, routeAt(rule, tt.in, "").effort, rule.String())
			}
			assert.Equal(t, tt.since, routeAt(leanR1, tt.in, "").reason)
		})
	}
}

func TestLeanRules_EscalationCap(t *testing.T) {
	in := newLeanInput("fix it").turn(testsFailed).turn(testsFailed)
	c := routeAt(leanR2, in, llm.ReasoningEffortXHigh)
	assert.Equal(t, llm.ReasoningEffortXHigh, c.effort)
	assert.Equal(t, "r2: failed twice: go test ./...", c.reason)
	c = routeAt(leanR2, in, llm.ReasoningEffortHigh)
	assert.Equal(t, llm.ReasoningEffortHigh, c.effort, "the model's highest effort caps it")
	assert.Equal(t, "r2: failed twice, at the highest effort: go test ./...", c.reason)

	l := &leanRouter{rule: leanR2, top: func(string) llm.ReasoningEffort { return "" }}
	c = l.route(in.items, llm.ReasoningEffortMax, true, "m")
	assert.Equal(t, llm.ReasoningEffortMax, c.effort, "nothing above ultra")
	assert.True(t, c.ultra)
	c = l.route(newLeanInput("x").turn(patched).items, llm.ReasoningEffortMax, true, "m")
	assert.Equal(t, effortChoice{effort: llm.ReasoningEffortMax, reason: "r2: confirmations: patch applied"}, c, "ultra lowers to max")
}

func TestLeanRules_ReadingHeavy(t *testing.T) {
	short := shell("touch x", "")
	in := newLeanInput("add a flag")
	for range 3 {
		in.turn(short)
	}
	assert.Equal(t, llm.ReasoningEffortMedium, routeAt(leanR3, in, "").effort, "three turns without an edit")
	in.turn(short)
	c := routeAt(leanR3, in, "")
	assert.Equal(t, llm.ReasoningEffortHigh, c.effort)
	assert.Equal(t, "r3: reading-heavy: no edit in 4 turns", c.reason)
	in.turn(patched)
	assert.Equal(t, llm.ReasoningEffortMedium, routeAt(leanR3, in, "").effort, "an edit flips it off")

	primed := newLeanInput("")
	primed.items = []llm.Item{msg(llm.RoleSystem, "sys"), msg(llm.RoleUser, primedOpen+"\nreview of nothing"), msg(llm.RoleUser, "add a flag")}
	primed.turn(short)
	assert.Equal(t, llm.ReasoningEffortMedium, routeAt(leanR3, primed, "").effort, "the workspace context is not the task")
	assert.Equal(t, "r3: reading-heavy: the task says why", routeAt(leanR3, newLeanInput("Why does it crash?").turn(short), "").reason)
}

func TestLowerAndRaiseEffort(t *testing.T) {
	for in, want := range map[llm.ReasoningEffort]llm.ReasoningEffort{
		"low": "low", "medium": "low", "high": "medium", "xhigh": "high", "max": "xhigh",
	} {
		assert.Equal(t, want, lowerEffort(in, false), in)
	}
	assert.Equal(t, llm.ReasoningEffortMax, lowerEffort(llm.ReasoningEffortMax, true), "ultra")
	up, ok := raiseEffort("high", false, "")
	assert.True(t, ok)
	assert.Equal(t, llm.ReasoningEffortXHigh, up)
	_, ok = raiseEffort("max", false, "")
	assert.False(t, ok)
	_, ok = raiseEffort("high", false, "high")
	assert.False(t, ok)
}
