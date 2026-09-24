package embedded_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/engine/embedded"
	"github.com/viktordanov/uagent-harness/internal/hooks"
	"github.com/viktordanov/uagent-harness/internal/mcp"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
	"github.com/viktordanov/uagent-harness/testing/harnesstest"
)

func mcpManager(t *testing.T, e *env, cfg mcp.ServerConfig) *mcp.Manager {
	t.Helper()
	cfg.Command = harnesstest.MCPServer(t)
	m, err := mcp.NewManager(map[string]mcp.ServerConfig{"test": cfg}, mcp.Options{Workspace: e.Workspace})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	return m
}

func (e *env) withMCP(m *mcp.Manager, runner *hooks.Runner) *embedded.Engine {
	return embedded.New(embedded.Config{StateDir: e.StateDir, Provider: "openai", Getenv: e.getenv, MCP: m, Hooks: runner})
}

func call(name, args string) fakellm.Call { return fakellm.Call{Name: name, Args: args} }

func TestEmbedded_MCPTools(t *testing.T) {
	e := newEnv(t,
		fakellm.Reply{Text: "Using tools.", Calls: []fakellm.Call{
			call("mcp__test__echo", `{"text":"hi"}`),
			call("mcp__test__image", `{}`),
			call("mcp__test__fail", ``),
			call("mcp__test__sleep", `{"ms":1500}`),
			call("mcp__test__crash", `{}`),
			call("mcp__test__structured", `{}`),
		}},
		fakellm.Reply{Text: "done"},
	)
	m := mcpManager(t, e, mcp.ServerConfig{
		ToolTimeoutSec: new(0.3), DisabledTools: []string{"crash"}, SupportsParallelToolCalls: true,
		Tools: map[string]mcp.ToolConfig{"structured": {ApprovalMode: mcp.ApprovalPrompt}},
	})
	runner, err := hooks.New([]hooks.Hook{{
		Event: hooks.PreToolUse, Matcher: "mcp__test__echo", Source: hooks.SourceUser,
		Command: `echo '{"hookSpecificOutput":{"updatedInput":{"text":"hooked"}}}'`,
	}}, nil, e.Workspace)
	require.NoError(t, err)
	s, err := session.Open(context.Background(), e.withMCP(m, runner), session.Options{Settings: e.settings(), Hooks: runner})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	ev := &events{t: t, s: s}

	_, err = s.Submit("use the tools")
	require.NoError(t, err)
	result := ev.finished()

	assert.Equal(t, core.StatusOK, result.Status)
	assert.Equal(t, "done", result.Answer)
	reqs := e.llm.Requests()
	require.GreaterOrEqual(t, len(reqs), 3, "results arrive as calls finish")
	assert.Contains(t, strings.Join(reqs[1].ToolOutputs, "\n"), "Tool call is still running", "the slow call did not hold up the model")
	last := reqs[len(reqs)-1]
	assert.Contains(t, reqs[0].Tools["mcp__test__echo"], `"text"`, "the model gets the MCP input schema")
	assert.Contains(t, reqs[0].Tools, "Bash")
	assert.NotContains(t, reqs[0].Tools, "mcp__test__crash", "disabled tools are hidden")
	outputs := strings.Join(last.ToolOutputs, "\n")
	for _, want := range []string{
		"echo: hooked", // PreToolUse matched the MCP name and rewrote the input
		"a pixel",
		"Error: it failed",
		"Error: the tool did not finish within 300ms",
		`tool "mcp__test__crash" is not available`,
		`needs the user's approval (approval_mode "prompt")`,
	} {
		assert.Contains(t, outputs, want)
	}
	require.Len(t, last.ToolImages, 1)
	assert.True(t, strings.HasPrefix(last.ToolImages[0], "data:image/png;base64,"))
	assert.Equal(t, 1, countKind[session.HookRan](ev.all))
}

func TestEmbedded_MCPServerCrashes(t *testing.T) {
	e := newEnv(t,
		fakellm.Reply{Calls: []fakellm.Call{call("mcp__test__crash", `{}`)}},
		fakellm.Reply{Calls: []fakellm.Call{call("mcp__test__echo", `{"text":"again"}`)}},
		fakellm.Reply{Text: "done"},
	)
	s, ev := e.open(t, e.withMCP(mcpManager(t, e, mcp.ServerConfig{}), nil), "")

	_, err := s.Submit("crash it")
	require.NoError(t, err)
	result := ev.finished()

	assert.Equal(t, core.StatusOK, result.Status, "a crashing server does not stop the run")
	reqs := e.llm.Requests()
	require.Len(t, reqs, 3)
	assert.Contains(t, reqs[1].ToolOutputs[0], "Error: failed to call crash on test")
	assert.Contains(t, reqs[2].ToolOutputs[1], "Error: the MCP server test failed: the server stopped")
}

func TestEmbedded_MCPResumesWithoutTheServer(t *testing.T) {
	e := newEnv(t,
		fakellm.Reply{Calls: []fakellm.Call{call("mcp__test__echo", `{"text":"before"}`)}},
		fakellm.Reply{Text: "first"},
		fakellm.Reply{Calls: []fakellm.Call{call("mcp__test__echo", `{"text":"after"}`)}},
		fakellm.Reply{Text: "second"},
	)
	s, ev := e.open(t, e.withMCP(mcpManager(t, e, mcp.ServerConfig{}), nil), "")
	_, err := s.Submit("one")
	require.NoError(t, err)
	assert.Equal(t, "first", ev.finished().Answer)
	require.NoError(t, s.Close())

	resumed, ev := e.open(t, e.embedded(), s.ID())
	_, err = resumed.Submit("two")
	require.NoError(t, err)
	result := ev.finished()

	assert.Equal(t, core.StatusOK, result.Status)
	assert.Equal(t, "second", result.Answer)
	reqs := e.llm.Requests()
	last := reqs[len(reqs)-1].ToolOutputs
	assert.Contains(t, last[0], "echo: before", "the stored result needs no server")
	assert.Contains(t, last[1], `tool "mcp__test__echo" is not available`)
}

func TestEmbedded_MCPInterruptThenContinue(t *testing.T) {
	e := newEnv(t, fakellm.Reply{Calls: []fakellm.Call{call("mcp__test__sleep", `{"ms":3000}`)}})
	s, ev := e.open(t, e.withMCP(mcpManager(t, e, mcp.ServerConfig{SupportsParallelToolCalls: true}), nil), "")

	_, err := s.Submit("wait a while")
	require.NoError(t, err)
	ev.until("the tool starting", isA[core.ToolStarted])
	started := time.Now()
	require.NoError(t, s.Interrupt())
	result := ev.finished()
	assert.Equal(t, core.StatusInterrupted, result.Status)
	assert.Less(t, time.Since(started), 2*time.Second, "the hard stop cancels the call")
	ev.idle()

	_, err = s.Submit("carry on")
	require.NoError(t, err)
	result = ev.finished()
	assert.Equal(t, core.StatusOK, result.Status)
	reqs := e.llm.Requests()
	assert.Contains(t, strings.Join(reqs[len(reqs)-1].ToolOutputs, "\n"), "Error: the MCP call was canceled")
}

// TestEmbedded_MCPApproval asks the user about a "prompt" tool through the
// same prompt as sandbox escalations: an approval runs it, a decline tells
// the model.
func TestEmbedded_MCPApproval(t *testing.T) {
	for _, tc := range []struct {
		answer approval.Answer
		want   string
	}{
		{approval.Approve, "echo: asked"},
		{approval.Decline, "the user declined the MCP tool mcp__test__echo"},
	} {
		t.Run(string(tc.answer), func(t *testing.T) {
			e := newEnv(t, fakellm.Reply{Calls: []fakellm.Call{call("mcp__test__echo", `{"text":"asked"}`)}}, fakellm.Reply{Text: "done"})
			m := mcpManager(t, e, mcp.ServerConfig{Tools: map[string]mcp.ToolConfig{"echo": {ApprovalMode: mcp.ApprovalPrompt}}})
			s, err := session.Open(context.Background(), e.withMCP(m, nil), session.Options{Settings: e.settings(), Interactive: true})
			require.NoError(t, err)
			t.Cleanup(func() { _ = s.Close() })
			ev := &events{t: t, s: s}

			_, err = s.Submit("echo")
			require.NoError(t, err)
			req := ev.until("ApprovalRequested", isA[session.ApprovalRequested]).(session.ApprovalRequested)
			assert.Equal(t, `mcp__test__echo {"text":"asked"}`, req.Command)
			require.NoError(t, s.Resolve(req.ID, tc.answer))
			ev.finished()

			reqs := e.llm.Requests()
			assert.Contains(t, strings.Join(reqs[len(reqs)-1].ToolOutputs, "\n"), tc.want)
		})
	}
}

// A huge result reaches the model bounded by the runner, as other tool
// output is; arguments that are not a JSON object are refused before
// anyone is asked to approve them.
func TestEmbedded_MCPBoundsOutputAndChecksArguments(t *testing.T) {
	e := newEnv(t,
		fakellm.Reply{Calls: []fakellm.Call{call("mcp__test__big", `{"n":2000000}`), call("mcp__test__echo", `not json`), call("mcp__test__echo", `[1]`)}},
		fakellm.Reply{Text: "waiting"},
		fakellm.Reply{Text: "done"},
	)
	m := mcpManager(t, e, mcp.ServerConfig{Tools: map[string]mcp.ToolConfig{"echo": {ApprovalMode: mcp.ApprovalPrompt}}})
	s, err := session.Open(context.Background(), e.withMCP(m, nil), session.Options{Settings: e.settings(), Interactive: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	ev := &events{t: t, s: s}

	_, err = s.Submit("go")
	require.NoError(t, err)
	assert.Equal(t, core.StatusOK, ev.finished().Status)
	assert.Zero(t, countKind[session.ApprovalRequested](ev.all), "nothing to approve")
	reqs := e.llm.Requests()
	var big []string
	outputs := reqs[len(reqs)-1].ToolOutputs
	for _, out := range outputs {
		if strings.Contains(out, "bytes truncated") {
			big = append(big, out)
		}
	}
	require.Len(t, big, 1, "the result arrived, bounded")
	assert.Less(t, len(big[0]), 60_000)
	assert.Equal(t, 2, strings.Count(strings.Join(outputs, "\n"), "the arguments must be a JSON object"))
}
