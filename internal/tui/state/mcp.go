package state

import (
	"fmt"
	"strings"

	"github.com/viktordanov/uagent-harness/internal/mcp"
	"github.com/viktordanov/uagent-harness/internal/session"
)

// EffListMCP asks the session for its MCP servers.
type EffListMCP struct{}

func (EffListMCP) effect() {}

// MCPListed reports the MCP servers for /mcp. Supported is false when the
// engine does not run MCP servers.
type MCPListed struct {
	Servers   []mcp.ServerStatus
	Supported bool
}

func cmdMCP(*State, string) []Effect { return []Effect{EffListMCP{}} }

// showMCP lists each server with its state and tools.
func (s *State) showMCP(e MCPListed) {
	switch {
	case !e.Supported:
		s.notice(session.LevelWarning, "MCP servers need the embedded engine")

		return
	case len(e.Servers) == 0:
		s.notice(session.LevelInfo, "no MCP servers are configured; add [mcp_servers.<name>] to the configuration")

		return
	}
	var b strings.Builder
	for i, srv := range e.Servers {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s · %s", srv.Name, srv.State)
		switch {
		case srv.Error != "":
			fmt.Fprintf(&b, ": %s", srv.Error)
		case srv.State == mcp.StateReady:
			fmt.Fprintf(&b, " · %d tools", len(srv.Tools))
			if len(srv.Tools) > 0 {
				b.WriteString(": " + strings.Join(srv.Tools, ", "))
			}
		}
	}
	s.notice(session.LevelInfo, b.String())
}
