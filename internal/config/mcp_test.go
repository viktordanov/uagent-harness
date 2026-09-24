package config_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/config"
	"github.com/viktordanov/uagent-harness/internal/mcp"
)

// codexConfig is a Codex [mcp_servers] section, as Codex documents it.
const codexConfig = `
[mcp_servers.docs]
command = "npx"
args = ["-y", "docs-mcp"]
env = { LANG_CODE = "en" }
env_vars = ["DOCS_TOKEN"]
cwd = "/tmp"
startup_timeout_sec = 20
tool_timeout_sec = 60.5
enabled_tools = []
disabled_tools = ["delete_page"]
supports_parallel_tool_calls = true
default_tools_approval_mode = "writes"

[mcp_servers.docs.tools.search]
approval_mode = "approve"

[mcp_servers.tracker]
url = "https://mcp.example.com/mcp"
bearer_token_env_var = "TRACKER_TOKEN"
http_headers = { "X-Client" = "uah" }
env_http_headers = { "X-Team" = "TEAM" }
enabled = false
required = true
startup_timeout_ms = 1500
`

func TestMCPServers(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "ws")
	user := filepath.Join(root, "config.toml")
	write(t, user, codexConfig+"\n[projects.\""+ws+"\"]\ntrusted = true\n")
	write(t, config.ProjectFile(ws), "[mcp_servers.tracker]\ncommand = \"tracker-mcp\"\n[mcp_servers.local]\ncommand = \"local-mcp\"\n")

	cfg, _, err := config.Load(user, ws)
	require.NoError(t, err)
	docs := cfg.MCPServers["docs"]
	require.NoError(t, docs.Validate())
	assert.Equal(t, []string{"-y", "docs-mcp"}, docs.Args)
	assert.NotNil(t, docs.EnabledTools, "an empty enabled_tools allows no tools")
	assert.False(t, docs.Allows("search"))
	assert.Equal(t, mcp.ApprovalApprove, docs.ApprovalFor("search"))
	assert.Equal(t, mcp.ApprovalWrites, docs.ApprovalFor("other"))
	assert.Equal(t, "tracker-mcp", cfg.MCPServers["tracker"].Command, "a project server replaces the user's whole")
	assert.Empty(t, cfg.MCPServers["tracker"].URL)
	assert.Equal(t, "local-mcp", cfg.MCPServers["local"].Command)

	user2 := filepath.Join(root, "user2.toml")
	write(t, user2, codexConfig)
	cfg, _, err = config.Load(user2, ws)
	require.NoError(t, err)
	tracker := cfg.MCPServers["tracker"]
	require.NoError(t, tracker.Validate())
	assert.False(t, tracker.IsEnabled())
	assert.Equal(t, "TEAM", tracker.EnvHTTPHeaders["X-Team"])

	write(t, user2, "[mcp_servers.x]\ncommand = \"x\"\noauth_resource = \"r\"\n")
	_, _, err = config.Load(user2, ws)
	require.ErrorContains(t, err, "unknown key", "unsupported Codex keys are errors, not ignored")
}
