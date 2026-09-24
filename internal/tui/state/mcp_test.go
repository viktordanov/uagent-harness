package state_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/mcp"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

func TestReduce_MCP(t *testing.T) {
	s, effects := apply(opened(), state.Submit{Text: "/mcp"})
	assert.Equal(t, []state.Effect{state.EffListMCP{}}, effects)

	s, _ = apply(s, state.MCPListed{Supported: true, Servers: []mcp.ServerStatus{
		{Name: "docs", State: mcp.StateReady, Tools: []string{"mcp__docs__search", "mcp__docs__get"}},
		{Name: "broken", State: mcp.StateFailed, Error: "did not start within 30s"},
	}})
	last := s.Items[len(s.Items)-1]
	require.Equal(t, state.KindNotice, last.Kind)
	assert.Equal(t, "docs · ready · 2 tools: mcp__docs__search, mcp__docs__get\nbroken · failed: did not start within 30s", last.Text)

	s, _ = apply(s, state.MCPListed{Supported: false})
	assert.Equal(t, "MCP servers need the embedded engine", s.Items[len(s.Items)-1].Text)
	s, _ = apply(s, state.MCPListed{Supported: true})
	assert.Contains(t, s.Items[len(s.Items)-1].Text, "no MCP servers are configured")
}
