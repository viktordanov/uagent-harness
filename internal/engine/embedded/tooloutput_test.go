package embedded_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/internal/engine"
	"github.com/viktordanov/uah/internal/mcp"
	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/testing/fakellm"
)

// TestToolOutput_LiveAndReloaded: a failed command's stderr and an MCP
// call's result reach the stream as engine.ToolOutput, and a reloaded
// transcript has the same events after the calls.
func TestToolOutput_LiveAndReloaded(t *testing.T) {
	e := newEnv(t,
		fakellm.Reply{Text: "Trying.", Commands: []string{"echo fine", "echo 'no such file' >&2; exit 3"}, Calls: []fakellm.Call{call("mcp__test__echo", `{"text":"hi"}`)}},
		fakellm.Reply{Text: "done"},
	)
	m := mcpManager(t, e, mcp.ServerConfig{})
	s, err := session.Open(context.Background(), e.withMCP(m, nil), session.Options{Settings: e.settings()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	ev := &events{t: t, s: s}
	_, err = s.Submit("try")
	require.NoError(t, err)
	ev.finished()

	outputs := func(events []any) map[string]engine.ToolOutput {
		out := map[string]engine.ToolOutput{}
		for _, e := range events {
			if o, ok := e.(engine.ToolOutput); ok {
				out[o.CallID] = o
			}
		}

		return out
	}
	var live []any
	for _, e := range ev.all {
		live = append(live, e)
	}
	got := outputs(live)
	require.Len(t, got, 2, "the failed command and the MCP call; not the command that passed")
	var stderr, result string
	for _, o := range got {
		stderr += o.Output
		result += o.Result
	}
	assert.Equal(t, "no such file\n", stderr)
	assert.Contains(t, result, "echo: hi")

	runs, err := session.Load(e.StateDir, s.ID())
	require.NoError(t, err)
	var reloaded []any
	for _, e := range runs[0].Events {
		reloaded = append(reloaded, e)
	}
	assert.Equal(t, got, outputs(reloaded))
}
