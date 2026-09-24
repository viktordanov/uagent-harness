package embedded_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/mcp"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
	"github.com/viktordanov/uagent-harness/testing/harnesstest"
)

// TestEmbedded_MCPAlwaysAllow answers "don't ask again for this tool": the
// call runs, the next call of the tool in the same run is not asked about,
// and the configuration file that has the server gets approval_mode
// approve for it.
func TestEmbedded_MCPAlwaysAllow(t *testing.T) {
	e := newEnv(t,
		fakellm.Reply{Calls: []fakellm.Call{call("mcp__test__echo", `{"text":"first"}`)}},
		fakellm.Reply{Calls: []fakellm.Call{call("mcp__test__echo", `{"text":"second"}`)}},
		fakellm.Reply{Text: "done"},
	)
	server := harnesstest.MCPServer(t)
	file := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(file, []byte("# mine\n[mcp_servers.test]\ncommand = \""+server+"\"\ndefault_tools_approval_mode = \"prompt\"\n"), 0o600))
	m, err := mcp.NewManager(map[string]mcp.ServerConfig{"test": {Command: server, DefaultToolsApprovalMode: mcp.ApprovalPrompt}}, mcp.Options{
		Workspace: e.Workspace, ServerFile: func(string) string { return file },
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })
	s, err := session.Open(context.Background(), e.withMCP(m, nil), session.Options{Settings: e.settings(), Interactive: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	ev := &events{t: t, s: s}

	_, err = s.Submit("echo twice")
	require.NoError(t, err)
	req := ev.until("ApprovalRequested", isA[session.ApprovalRequested]).(session.ApprovalRequested)
	assert.Equal(t, "mcp__test__echo", req.MCPTool)
	require.NoError(t, s.Resolve(req.ID, approval.ApproveTool))
	ev.finished()

	assert.Equal(t, 1, countKind[session.ApprovalRequested](ev.all), "the second call was not asked about")
	reqs := e.llm.Requests()
	require.Len(t, reqs, 3)
	assert.Contains(t, strings.Join(reqs[1].ToolOutputs, "\n"), "echo: first")
	assert.Contains(t, strings.Join(reqs[2].ToolOutputs, "\n"), "echo: second")
	data, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(data), "# mine\n"), "the file keeps its comments")
	assert.Contains(t, string(data), "[mcp_servers.test.tools.echo]\napproval_mode = \"approve\"\n")
	mode, ok := m.ToolApproval("mcp__test__echo")
	assert.True(t, ok)
	assert.Equal(t, mcp.ApprovalApprove, mode)
}
