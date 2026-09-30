package state_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"
	uaharness "github.com/viktordanov/uagent/harness"

	"github.com/viktordanov/uagent-harness/internal/cmdparse"
	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

func inWorkspace() state.State {
	s, _ := state.Reduce(state.New(t0), session.SessionOpened{At: t0, ID: "s", Settings: session.Settings{Workspace: "/w"}})
	s.Home = "/home/me"

	return s
}

func called(id, name, args string) core.ToolCalled {
	return core.ToolCalled{At: t0, CallID: id, Name: name, Label: args, Arguments: args}
}

func item(t *testing.T, s state.State, key string) state.Item {
	t.Helper()
	for _, it := range s.Items {
		if it.Key == key {
			return it
		}
	}
	require.Failf(t, "no item", key)

	return state.Item{}
}

// TestToolCalled_Shapes: a command is shaped from its whole arguments, not
// the runner's label cut at 120 characters, and an MCP call from its name
// and arguments in order.
func TestToolCalled_Shapes(t *testing.T) {
	command := "rtk proxy sh -c 'cat /w/a.go /home/me/notes.md'"
	args, err := json.Marshal(map[string]string{"command": command})
	require.NoError(t, err)
	s := fold(inWorkspace(),
		core.ToolCalled{At: t0, CallID: "c", Name: "Bash", Label: "cut", Arguments: string(args)},
		called("m", "mcp__docs__search", `{"query":"a b","limit":5,"tags":["x"]}`),
	)
	c := item(t, s, "call:c")
	assert.Equal(t, command, c.Command)
	assert.Equal(t, "READ", c.Verb)
	assert.Equal(t, "a.go, ~/notes.md", cmdparse.Summary{Parts: c.Parts}.Text())
	m := item(t, s, "call:m")
	assert.Equal(t, `docs · search  query "a b", limit 5, tags ["x"]`, cmdparse.Summary{Parts: m.Parts}.Text())
}

// TestToolCalled_LoadedWorkspace: before the session opens, a loaded run's
// commands show paths relative to that run's workspace.
func TestToolCalled_LoadedWorkspace(t *testing.T) {
	res := core.Result{Request: core.Request{RunID: "r1", Workspace: "/w"}}
	s, _ := state.Reduce(state.New(t0), state.HistoryLoaded{SessionID: "s", Runs: []session.LoadedRun{{
		Record: uaharness.RunRecord{Result: res, Complete: true},
		Events: []core.Event{called("c", "Bash", `{"command":"cat /w/a.go"}`)},
	}}})
	assert.Equal(t, "a.go", cmdparse.Summary{Parts: item(t, s, "call:c").Parts}.Text())
}

// TestSkills_Join: skills loaded one after another share the first one's
// line; one that fails gets its own line back.
func TestSkills_Join(t *testing.T) {
	s := fold(inWorkspace(),
		called("a", "SkillUse", `{"name":"one"}`),
		called("b", "SkillUse", `{"name":"two"}`),
		called("c", "SkillUse", `{"name":"three"}`),
	)
	assert.Equal(t, []string{"one", "two", "three"}, item(t, s, "call:a").Group)
	assert.Equal(t, "call:a", item(t, s, "call:c").MergedInto)

	s = fold(s, core.ToolFinished{At: t0, CallID: "b", OpID: "b", OK: false, Detail: "failed"})
	assert.Equal(t, []string{"one", "three"}, item(t, s, "call:a").Group)
	assert.Empty(t, item(t, s, "call:b").MergedInto)
}

// TestToolOutput: a failed command's last meaningful line and an MCP
// call's result summary land on their calls.
func TestToolOutput(t *testing.T) {
	s := fold(inWorkspace(),
		called("c", "Bash", `{"command":"go test ./..."}`),
		called("m", "mcp__docs__search", `{}`),
		called("n", "mcp__docs__list", `{}`),
		engine.ToolOutput{At: t0, CallID: "c", Output: "--- FAIL: TestX\nFAIL\tpkg\t0.1s\n\x1b[31m\x1b[0m\n  \n"},
		engine.ToolOutput{At: t0, CallID: "m", Result: `[{"a":1},{"a":2}]`, Size: 17},
		engine.ToolOutput{At: t0, CallID: "n", Result: "Found 3 docs\nmore", Size: 4096},
	)
	assert.Equal(t, "FAIL\tpkg\t0.1s", item(t, s, "call:c").ErrorLine)
	assert.Equal(t, "2 items", item(t, s, "call:m").Result)
	assert.Equal(t, "Found 3 docs · 4.0 KB", item(t, s, "call:n").Result)
}

func fold(s state.State, evs ...any) state.State {
	for _, e := range evs {
		s, _ = state.Reduce(s, e)
	}

	return s
}
