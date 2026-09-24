package mcp_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/mcp"
)

const userFile = `# my settings
model = "gpt-6-luna" # inline comment

[mcp_servers.docs]
command = "docs-mcp"
args = [
  "--verbose", # a flag
]

[mcp_servers.docs.tools.search]
approval_mode = "approve"

# the tracker, keep this comment
[mcp_servers.tracker]
url = "https://mcp.example.com/mcp"

[tui]
details = true
`

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(b)
}

// TestRemoveServer cuts the server and its sub-tables and keeps every
// other byte, comments included.
func TestRemoveServer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(userFile), 0o640))

	ok, err := mcp.RemoveServer(path, "docs")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, `# my settings
model = "gpt-6-luna" # inline comment

# the tracker, keep this comment
[mcp_servers.tracker]
url = "https://mcp.example.com/mcp"

[tui]
details = true
`, readFile(t, path))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm(), "the file keeps its permissions")

	ok, err = mcp.RemoveServer(path, "docs")
	require.NoError(t, err)
	assert.False(t, ok, "nothing to remove")

	ok, err = mcp.RemoveServer(path, "tracker")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "# my settings\nmodel = \"gpt-6-luna\" # inline comment\n\n# the tracker, keep this comment\n[tui]\ndetails = true\n", readFile(t, path))
}

func TestAddServer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "uagent", "config.toml")
	require.NoError(t, mcp.AddServer(path, "docs", mcp.ServerConfig{Command: "npx", Args: []string{"-y", "docs-mcp"}, Env: map[string]string{"LANG_CODE": "en"}}))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "a new file is private")

	require.NoError(t, os.WriteFile(path, []byte(userFile), 0o600))
	require.NoError(t, mcp.AddServer(path, "docs", mcp.ServerConfig{Command: "npx", Args: []string{"-y", "docs-mcp"}, Env: map[string]string{"LANG_CODE": "en"}}))
	require.NoError(t, mcp.AddServer(path, "remote.v2", mcp.ServerConfig{
		URL: "https://r.example.com/mcp", OAuthResource: "https://r.example.com", OAuth: &mcp.OAuthConfig{ClientID: "abc"},
	}))
	got := readFile(t, path)
	assert.Contains(t, got, "# my settings\nmodel = \"gpt-6-luna\" # inline comment\n\n# the tracker, keep this comment\n[mcp_servers.tracker]")
	assert.NotContains(t, got, "--verbose", "the old docs server is replaced whole")
	assert.Contains(t, got, "[mcp_servers.\"remote.v2\"]\n")

	var cfg struct {
		MCPServers map[string]mcp.ServerConfig `toml:"mcp_servers"`
		Model      string                      `toml:"model"`
		TUI        struct{ Details bool }      `toml:"tui"`
	}
	_, err = toml.Decode(got, &cfg)
	require.NoError(t, err)
	assert.Equal(t, "gpt-6-luna", cfg.Model)
	assert.Equal(t, []string{"-y", "docs-mcp"}, cfg.MCPServers["docs"].Args)
	assert.Equal(t, "en", cfg.MCPServers["docs"].Env["LANG_CODE"])
	assert.Empty(t, cfg.MCPServers["docs"].Tools, "its tool settings went with it")
	assert.Equal(t, "abc", cfg.MCPServers["remote.v2"].OAuth.ClientID)
	assert.Equal(t, "https://mcp.example.com/mcp", cfg.MCPServers["tracker"].URL)

	require.ErrorContains(t, mcp.AddServer(path, "bad name", mcp.ServerConfig{Command: "x"}), "invalid server name 'bad name'")
	require.ErrorContains(t, mcp.AddServer(path, "x", mcp.ServerConfig{}), "set command")
}

// A server written as an inline table cannot be cut; the file stays as it was.
func TestEditRefusesInlineServers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	inline := "mcp_servers = { docs = { command = \"docs-mcp\" } }\n"
	require.NoError(t, os.WriteFile(path, []byte(inline), 0o600))
	_, err := mcp.RemoveServer(path, "docs")
	require.ErrorContains(t, err, "is not written as its own [mcp_servers.docs] table")
	require.Error(t, mcp.AddServer(path, "docs", mcp.ServerConfig{Command: "x"}))
	assert.Equal(t, inline, readFile(t, path))

	require.NoError(t, os.WriteFile(path, []byte("[mcp_servers]\ndocs.command = \"docs-mcp\"\n"), 0o600))
	_, err = mcp.RemoveServer(path, "docs")
	require.Error(t, err)
	require.Error(t, mcp.AddServer(path, "docs", mcp.ServerConfig{Command: "x"}))

	require.NoError(t, os.WriteFile(path, []byte("not [toml"), 0o600))
	require.ErrorContains(t, mcp.AddServer(path, "docs", mcp.ServerConfig{Command: "x"}), "failed to parse")
}
