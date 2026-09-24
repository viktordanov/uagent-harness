package app_test

import (
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
)

func TestSetupMCP(t *testing.T) {
	_, in := setupEnv(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(in.ConfigPath), 0o700))
	write := func(content string) {
		require.NoError(t, os.WriteFile(in.ConfigPath, []byte(content), 0o600))
	}
	write("[mcp_servers.test]\ncommand = \"" + harnesstest.MCPServer(t) + "\"\n")

	res, err := app.Setup(in, io.Discard)
	require.NoError(t, err)
	lister, ok := res.Engine.(engine.MCPLister)
	require.True(t, ok, "the embedded engine lists MCP servers")
	closer, ok := res.Engine.(io.Closer)
	require.True(t, ok)
	t.Cleanup(func() { _ = closer.Close() })
	require.Eventually(t, func() bool { return lister.MCPServers()[0].State == mcp.StateReady }, 20*time.Second, 20*time.Millisecond)
	assert.Contains(t, lister.MCPServers()[0].Tools, "mcp__test__echo")

	in.Engine = app.EngineProcess
	in.Runner = harnesstest.FakeRunner(t)
	res, err = app.Setup(in, io.Discard)
	require.NoError(t, err)
	assert.Contains(t, res.Options.Notices, "MCP servers need the embedded engine; they do not start on the process engine")

	in.Engine = app.EngineEmbedded
	write("[mcp_servers.bad]\nurl = \"http://x\"\nargs = [\"a\"]\n")
	_, err = app.Setup(in, io.Discard)
	require.ErrorContains(t, err, "mcp_servers.bad: args is not supported for streamable_http")
}
