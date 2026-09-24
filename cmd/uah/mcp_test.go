package main_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/testing/harnesstest"
	"github.com/viktordanov/uagent-harness/testing/oauthserver"
)

func mcpEnv(t *testing.T) (user string, env []string) {
	t.Helper()
	root := t.TempDir()
	user = filepath.Join(root, "config", "uagent", "config.toml")
	writeFile(t, user, "# keep me\nmodel = \"gpt-6-luna\"\nmcp_oauth_credentials_store = \"file\"\n")

	return user, []string{"XDG_CONFIG_HOME=" + filepath.Join(root, "config"), "UAGENT_CONFIG=" + user}
}

// TestMCPCommands adds, lists, gets, and removes servers in the user file,
// keeping the rest of it.
func TestMCPCommands(t *testing.T) {
	user, env := mcpEnv(t)
	server := harnesstest.MCPServer(t)
	ws := t.TempDir()

	res := uahWith(t, env, "", "mcp", "add", "docs", "--env", "LANG_CODE=en", "--", server, "--flag")
	require.Equal(t, 0, res.code, res.stderr)
	assert.Equal(t, "Added MCP server 'docs' to "+user+".\n", res.stdout)
	res = uahWith(t, env, "", "mcp", "add", "remote", "--url", "https://mcp.example.com/mcp", "--bearer-token-env-var", "TOKEN")
	require.Equal(t, 0, res.code, res.stderr)

	res = uahWith(t, env, "", "mcp", "list", "-C", ws)
	require.Equal(t, 0, res.code, res.stderr)
	assert.Equal(t, strings.Join([]string{
		"Name  Command" + strings.Repeat(" ", len(server)-5) + "Args    Env              Cwd  Status   Auth",
		"docs  " + server + "  --flag  LANG_CODE=*****  -    enabled  Unsupported",
		"",
		"Name    Url                          Bearer Token Env Var  Status   Auth",
		"remote  https://mcp.example.com/mcp  TOKEN                 enabled  Bearer token",
		"",
	}, "\n"), res.stdout)

	res = uahWith(t, env, "", "mcp", "list", "--json", "-C", ws)
	require.Equal(t, 0, res.code, res.stderr)
	var list []map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.stdout), &list))
	require.Len(t, list, 2)
	assert.Equal(t, "stdio", list[0]["transport"].(map[string]any)["type"])
	assert.Equal(t, "bearer_token", list[1]["auth_status"])

	res = uahWith(t, env, "", "mcp", "get", "remote", "-C", ws)
	require.Equal(t, 0, res.code, res.stderr)
	assert.Contains(t, res.stdout, "remote\n  enabled: true\n  transport: streamable_http\n  url: https://mcp.example.com/mcp\n")
	assert.Contains(t, res.stdout, "  remove: uah mcp remove remote\n")

	res = uahWith(t, env, "", "mcp", "remove", "docs")
	require.Equal(t, 0, res.code, res.stderr)
	assert.Equal(t, "Removed MCP server 'docs'.\n", res.stdout)
	content, err := os.ReadFile(user)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(content), "# keep me\nmodel = \"gpt-6-luna\"\n"), string(content))
	assert.NotContains(t, string(content), "docs")

	for _, args := range [][]string{
		{"mcp", "add", "bad name", "--", "x"},
		{"mcp", "add", "x"},
		{"mcp", "add", "x", "--url", "https://x", "--env", "A=b"},
		{"mcp", "get", "nope"},
		{"mcp", "login", "nope"},
	} {
		res = uahWith(t, env, "", append(args, "-C", ws)...)
		assert.Equal(t, 2, res.code, "%v: %s", args, res.stderr)
	}
}

// TestMCPLogin logs in through the CLI with the browser step done by the
// test, then logs out.
func TestMCPLogin(t *testing.T) {
	_, env := mcpEnv(t)
	srv := oauthserver.New(t)
	ws := t.TempDir()

	res := uahWith(t, env, "", "mcp", "add", "remote", "--url", srv.MCPURL())
	require.Equal(t, 0, res.code, res.stderr)
	assert.Contains(t, res.stdout, "The server supports OAuth. Run `uah mcp login remote` to log in.")
	res = uahWith(t, env, "", "mcp", "list", "-C", ws)
	assert.Contains(t, res.stdout, "Not logged in")

	cmd := exec.Command(uahBin, "mcp", "login", "remote", "--no-browser", "-C", ws)
	cmd.Env = append(os.Environ(), env...)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Start())
	var out strings.Builder
	lines := bufio.NewScanner(stdout)
	for lines.Scan() {
		out.WriteString(lines.Text() + "\n")
		if strings.HasPrefix(lines.Text(), srv.URL()+"/authorize?") {
			require.NoError(t, oauthserver.Browser(lines.Text()))
		}
	}
	require.NoError(t, cmd.Wait(), stderr.String())
	assert.Contains(t, out.String(), "Authorize `remote` by opening this URL in your browser:")
	assert.Contains(t, out.String(), "Successfully logged in to MCP server 'remote'.")

	res = uahWith(t, env, "", "mcp", "list", "-C", ws)
	assert.Contains(t, res.stdout, "OAuth")
	res = uahWith(t, env, "", "mcp", "logout", "remote", "-C", ws)
	require.Equal(t, 0, res.code, res.stderr)
	assert.Equal(t, "Removed OAuth credentials for 'remote'.\n", res.stdout)
	res = uahWith(t, env, "", "mcp", "logout", "remote", "-C", ws)
	assert.Equal(t, "No OAuth credentials stored for 'remote'.\n", res.stdout)
}
