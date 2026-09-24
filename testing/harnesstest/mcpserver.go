package harnesstest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

var (
	mcpOnce sync.Once
	mcpPath string
	errMCP  error
)

// MCPServer builds the test MCP server (testing/mcpserver) once per test
// binary and returns its path.
func MCPServer(tb testing.TB) string {
	tb.Helper()
	mcpOnce.Do(func() {
		dir, err := os.MkdirTemp("", "uah-mcpserver") //nolint:usetesting // shared by every test in the binary
		if err != nil {
			errMCP = err

			return
		}
		mcpPath = filepath.Join(dir, "mcpserver")
		build := exec.CommandContext(context.Background(), "go", "build", "-o", mcpPath, "github.com/viktordanov/uagent-harness/testing/mcpserver")
		if out, err := build.CombinedOutput(); err != nil {
			errMCP = fmt.Errorf("build mcpserver: %w\n%s", err, out)
		}
	})
	require.NoError(tb, errMCP)

	return mcpPath
}
