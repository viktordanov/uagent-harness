package app

import (
	"fmt"
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
// With userFile set, "don't ask again" for a tool is saved to the file that
// configures its server.
func mcpManager(cfg config.Config, workspace string, logger *slog.Logger, userFile string) (*mcp.Manager, error) {
	if len(cfg.MCPServers) == 0 {
		return nil, nil //nolint:nilnil // no servers, no manager
	}
	store, err := mcpCredentials(cfg)
	if err != nil {
		return nil, err
	}
	opts := mcp.Options{Workspace: workspace, Logger: logger, Credentials: store}
	if userFile != "" {
		opts.ServerFile = func(server string) string {
			path, _ := MCPServerFile(userFile, workspace, server)

			return path
		}
	}
	m, err := mcp.NewManager(cfg.MCPServers, opts)
	if err != nil {
		return nil, usage(err)
	}

	return m, nil
}

// MCPServerFile is the configuration file that configures the MCP server:
// the trusted project file when it has the server, else the user file, as
// Codex saves a tool's approval where its server is configured. It fails
// when neither file has the server.
func MCPServerFile(userFile, workspace, server string) (string, error) {
	l, err := config.LoadLayers(userFile, workspace)
	if err != nil {
		return "", usage(err)
	}
	if _, ok := l.Project.MCPServers[server]; ok && l.ProjectFile != "" {
		return l.ProjectFile, nil
	}
	if _, ok := l.User.MCPServers[server]; ok {
		return userFile, nil
	}

	return "", usage(fmt.Errorf("no MCP server named '%s' is configured", server))
}
