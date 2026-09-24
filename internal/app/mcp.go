package app

import (
	"log/slog"
	"path/filepath"

	"github.com/viktordanov/uagent-harness/internal/config"
	"github.com/viktordanov/uagent-harness/internal/mcp"
)

// MCPCredentialsFile holds MCP OAuth logins when they are not in the OS
// keyring (mcp_oauth_credentials_store "file", or "auto" without a
// keyring), as Codex's .credentials.json.
func MCPCredentialsFile() string { return filepath.Join(config.Dir(), "mcp-credentials.json") }

// mcpCredentials is the configured store for MCP OAuth logins.
func mcpCredentials(cfg config.Config) (mcp.CredentialStore, error) {
	store, err := mcp.NewCredentialStore(cfg.MCPOAuthCredentialsStore, MCPCredentialsFile())
	if err != nil {
		return nil, usage(err)
	}

	return store, nil
}

// mcpManager builds the manager for the configured MCP servers (nil when
// there are none). It starts nothing: the embedded engine starts the
// servers on its first run. Servers' standard error is logged line by line.
func mcpManager(cfg config.Config, workspace string, logger *slog.Logger) (*mcp.Manager, error) {
	if len(cfg.MCPServers) == 0 {
		return nil, nil //nolint:nilnil // no servers, no manager
	}
	store, err := mcpCredentials(cfg)
	if err != nil {
		return nil, err
	}
	m, err := mcp.NewManager(cfg.MCPServers, mcp.Options{Workspace: workspace, Logger: logger, Credentials: store})
	if err != nil {
		return nil, usage(err)
	}

	return m, nil
}
