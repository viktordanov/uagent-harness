package app

import (
	"io"

	"github.com/viktordanov/uagent-harness/internal/config"
	"github.com/viktordanov/uagent-harness/internal/mcp"
)

// mcpManager builds the manager for the configured MCP servers (nil when
// there are none). It starts nothing: the embedded engine starts the
// servers on its first run. Servers' standard error goes to logOutput.
func mcpManager(cfg config.Config, workspace string, logOutput io.Writer) (*mcp.Manager, error) {
	if len(cfg.MCPServers) == 0 {
		return nil, nil //nolint:nilnil // no servers, no manager
	}
	m, err := mcp.NewManager(cfg.MCPServers, mcp.Options{Workspace: workspace, Stderr: logOutput})
	if err != nil {
		return nil, usage(err)
	}

	return m, nil
}
