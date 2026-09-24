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
	_, effects = apply(s, state.Submit{Text: "/mcp verbose"})
	assert.Equal(t, []state.Effect{state.EffListMCP{Verbose: true}}, effects)
	s, effects = apply(s, state.Submit{Text: "/mcp all"})
	assert.Empty(t, effects)
	assert.Equal(t, "Usage: /mcp [verbose]", s.Items[len(s.Items)-1].Text)

	servers := []mcp.ServerStatus{
		{Name: "docs", State: mcp.StateReady, Tools: []mcp.Tool{{Name: "mcp__docs__search"}, {Name: "mcp__docs__get"}}},
		{Name: "broken", State: mcp.StateFailed, Error: "did not start within 30s"},
	}
	s, _ = apply(s, state.MCPListed{Supported: true, Verbose: true, Servers: servers})
	last := s.Items[len(s.Items)-1]
	require.Equal(t, state.KindMCP, last.Kind)
	assert.Equal(t, servers, last.MCP)
	assert.True(t, last.Final, "verbose")

	s, _ = apply(s, state.MCPListed{Supported: false})
	assert.Equal(t, "MCP servers need the embedded engine", s.Items[len(s.Items)-1].Text)
	s, _ = apply(s, state.MCPListed{Supported: true})
	assert.Contains(t, s.Items[len(s.Items)-1].Text, "no MCP servers are configured")
}
