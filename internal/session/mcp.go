package session

import (
	"fmt"
	"time"

	"github.com/viktordanov/uah/internal/engine"
	"github.com/viktordanov/uah/internal/mcp"
)

// MCPStarted reports the MCP servers once an interactive session has
// connected them, before any message: each server is ready, failed, needs a
// login, or is disabled. Runs use these connections.
type MCPStarted struct {
	At      time.Time
	Servers []mcp.ServerStatus
}

func (e MCPStarted) OccurredAt() time.Time { return e.At }

// startMCP connects the engine's MCP servers as the session opens, so a
// host that waits for a server's initialize before its first message does
// not wait forever. A server that did not start gets a notice, then
// MCPStarted reports them all.
func (s *Session) startMCP() {
	st, ok := s.eng.(engine.MCPStarter)
	if !ok {
		return
	}
	go func() {
		servers := st.StartMCP(s.ctx)
		if len(servers) == 0 {
			return
		}
		s.post(evDo(func() { s.onMCPStarted(servers) }))
	}()
}

func (s *Session) onMCPStarted(servers []mcp.ServerStatus) {
	for _, sv := range servers {
		if sv.State != mcp.StateFailed && sv.State != mcp.StateNeedsLogin {
			continue
		}
		n := Notice{At: time.Now(), Level: LevelWarning, Message: fmt.Sprintf("MCP server %s did not start: %s", sv.Name, sv.Error)}
		if sv.Required {
			n.Level, n.Message = LevelError, fmt.Sprintf("the required MCP server %s did not start, so messages fail: %s", sv.Name, sv.Error)
		}
		s.emit(n)
	}
	s.emit(MCPStarted{At: time.Now(), Servers: servers})
}
