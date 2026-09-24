package state

import (
	"github.com/viktordanov/uagent-harness/internal/mcp"
	"github.com/viktordanov/uagent-harness/internal/session"
)

// EffListMCP asks the session for its MCP servers.
type EffListMCP struct {
	Verbose bool
}

func (EffListMCP) effect() {}

// MCPListed reports the MCP servers for /mcp. Supported is false when the
// engine does not run MCP servers; Verbose lists every tool.
type MCPListed struct {
	Servers   []mcp.ServerStatus
	Supported bool
	Verbose   bool
}

// cmdMCP is Codex's /mcp: a line per server, and with "verbose" its
// transport, auth, and tools.
func cmdMCP(s *State, args string) []Effect {
	switch args {
	case "":
		return []Effect{EffListMCP{}}
	case "verbose":
		return []Effect{EffListMCP{Verbose: true}}
	}
	s.notice(session.LevelWarning, "Usage: /mcp [verbose]")

	return nil
}

// showMCP adds the MCP panel: a KindMCP item holding the servers, drawn
// compact unless Final (verbose) or the detailed view is on.
func (s *State) showMCP(e MCPListed) {
	switch {
	case !e.Supported:
		s.notice(session.LevelWarning, "MCP servers need the embedded engine")

		return
	case len(e.Servers) == 0:
		s.notice(session.LevelInfo, "no MCP servers are configured; add one with `uah mcp add` or [mcp_servers.<name>] in the configuration")

		return
	}
	s.put(Item{Kind: KindMCP, Key: s.nextKey("mcp"), MCP: e.Servers, Final: e.Verbose})
}
