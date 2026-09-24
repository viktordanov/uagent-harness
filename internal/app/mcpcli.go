package app

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/viktordanov/uagent-harness/internal/config"
	"github.com/viktordanov/uagent-harness/internal/mcp"
)

// MCPEntry is one configured server as `uah mcp list` and `get` show it.
type MCPEntry struct {
	Name   string
	Config mcp.ServerConfig
	Auth   mcp.AuthStatus
}

// MCPServers lists the servers a session in the workspace would use (the
// user file and a trusted project file), with each one's auth status. It
// starts no server; an HTTP server without a stored login is probed for
// OAuth, at most 5 s, all at once.
func MCPServers(ctx context.Context, configPath, workspace string) ([]MCPEntry, error) {
	cfg, store, err := loadMCP(configPath, workspace)
	if err != nil {
		return nil, err
	}
	names := slices.Sorted(maps.Keys(cfg.MCPServers))
	out := make([]MCPEntry, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		out[i] = MCPEntry{Name: name, Config: cfg.MCPServers[name]}
		wg.Go(func() { out[i].Auth = mcp.AuthStatusOf(ctx, name, out[i].Config, store, nil) })
	}
	wg.Wait()

	return out, nil
}

// MCPLogin logs in to a configured HTTP server with OAuth and stores the
// tokens where mcp_oauth_credentials_store says.
func MCPLogin(ctx context.Context, configPath, workspace, name string, opts mcp.LoginOptions) error {
	cfg, store, err := loadMCP(configPath, workspace)
	if err != nil {
		return err
	}
	server, err := findServer(cfg, name)
	if err != nil {
		return err
	}
	opts.Store = store
	opts.Settings = mcp.OAuthSettings{CallbackPort: cfg.MCPOAuthCallbackPort, CallbackURL: cfg.MCPOAuthCallbackURL}

	return mcp.Login(ctx, name, server, opts) //nolint:wrapcheck // Login's errors name the server
}

// MCPLogout forgets a server's OAuth login; ok is false when none was stored.
func MCPLogout(configPath, workspace, name string) (bool, error) {
	cfg, store, err := loadMCP(configPath, workspace)
	if err != nil {
		return false, err
	}
	server, err := findServer(cfg, name)
	if err != nil {
		return false, err
	}

	return mcp.Logout(name, server, store) //nolint:wrapcheck // Logout's errors are the user's to read
}

func loadMCP(configPath, workspace string) (config.Config, mcp.CredentialStore, error) {
	cfg, _, err := config.Load(configPath, workspace)
	if err != nil {
		return config.Config{}, nil, usage(err)
	}
	for _, name := range slices.Sorted(maps.Keys(cfg.MCPServers)) {
		if err := cfg.MCPServers[name].Validate(); err != nil {
			return config.Config{}, nil, usage(fmt.Errorf("mcp_servers.%s: %w", name, err))
		}
	}
	store, err := mcpCredentials(cfg)
	if err != nil {
		return config.Config{}, nil, err
	}

	return cfg, store, nil
}

func findServer(cfg config.Config, name string) (mcp.ServerConfig, error) {
	server, ok := cfg.MCPServers[name]
	if !ok {
		return mcp.ServerConfig{}, usage(fmt.Errorf("No MCP server named '%s' found.", name)) //nolint:staticcheck // Codex's message
	}

	return server, nil
}
