package embedded_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

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
