package app_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/app"
	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/mcp"
	"github.com/viktordanov/uagent-harness/testing/harnesstest"
	"github.com/viktordanov/uagent-harness/testing/oauthserver"
)

func TestSetupMCP(t *testing.T) {
	_, in := setupEnv(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(in.ConfigPath), 0o700))
	write := func(content string) {
		require.NoError(t, os.WriteFile(in.ConfigPath, []byte(content), 0o600))
	}
	write("[mcp_servers.test]\ncommand = \"" + harnesstest.MCPServer(t) + "\"\n")

	res, err := app.Setup(context.Background(), in, io.Discard)
	require.NoError(t, err)
	lister, ok := res.Engine.(engine.MCPLister)
	require.True(t, ok, "the embedded engine lists MCP servers")
	closer, ok := res.Engine.(io.Closer)
	require.True(t, ok)
	t.Cleanup(func() { _ = closer.Close() })
	require.Eventually(t, func() bool { return lister.MCPServers()[0].State == mcp.StateReady }, 20*time.Second, 20*time.Millisecond)
	assert.Equal(t, "mcp__test__add_tool", lister.MCPServers()[0].Tools[0].Name)

	in.Engine = app.EngineProcess
	in.Runner = harnesstest.FakeRunner(t)
	res, err = app.Setup(context.Background(), in, io.Discard)
	require.NoError(t, err)
	assert.Contains(t, res.Options.Notices, "MCP servers need the embedded engine; they do not start on the process engine")

	in.Engine = app.EngineEmbedded
	write("[mcp_servers.bad]\nurl = \"http://x\"\nargs = [\"a\"]\n")
	_, err = app.Setup(context.Background(), in, io.Discard)
	require.ErrorContains(t, err, "mcp_servers.bad: args is not supported for streamable_http")
}

// TestDoctor_MCPNeedsLogin warns about a server that asks for OAuth, with
// the command that fixes it, and fails when the server is required.
func TestDoctor_MCPNeedsLogin(t *testing.T) {
	_, in := setupEnv(t)
	srv := oauthserver.New(t)
	for required, want := range map[bool]app.CheckStatus{false: app.CheckWarn, true: app.CheckFail} {
		writeConfig(t, &in, fmt.Sprintf("mcp_oauth_credentials_store = \"file\"\n[mcp_servers.remote]\nurl = %q\nrequired = %t\n", srv.MCPURL(), required))
		checks := app.Doctor(context.Background(), in, app.DoctorOptions{HookTrustFile: filepath.Join(t.TempDir(), "trust.json")})
		c := find(t, checks, "mcp remote")
		assert.Equal(t, want, c.Status, "required=%t", required)
		assert.Equal(t, "needs login", c.Detail)
		assert.Equal(t, "run `uah mcp login remote`", c.Fix)
	}
}

// TestMCPServersAndLogin lists servers with their auth status and logs in
// with the configured credential store.
func TestMCPServersAndLogin(t *testing.T) {
	_, in := setupEnv(t)
	srv := oauthserver.New(t)
	writeConfig(t, &in, fmt.Sprintf("mcp_oauth_credentials_store = \"file\"\n[mcp_servers.remote]\nurl = %q\n[mcp_servers.local]\ncommand = \"x\"\n", srv.MCPURL()))
	ctx := context.Background()

	entries, err := app.MCPServers(ctx, in.ConfigPath, in.Workspace)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, mcp.AuthUnsupported, entries[0].Auth, "local")
	assert.Equal(t, mcp.AuthNotLoggedIn, entries[1].Auth, "remote")

	require.NoError(t, app.MCPLogin(ctx, in.ConfigPath, in.Workspace, "remote", mcp.LoginOptions{OpenBrowser: oauthserver.Browser}))
	_, err = os.Stat(app.MCPCredentialsFile())
	require.NoError(t, err, "the file store in the config directory")
	entries, err = app.MCPServers(ctx, in.ConfigPath, in.Workspace)
	require.NoError(t, err)
	assert.Equal(t, mcp.AuthOAuth, entries[1].Auth)

	ok, err := app.MCPLogout(in.ConfigPath, in.Workspace, "remote")
	require.NoError(t, err)
	assert.True(t, ok)
	_, err = app.MCPLogout(in.ConfigPath, in.Workspace, "nope")
	require.ErrorContains(t, err, "No MCP server named 'nope' found.")
}
