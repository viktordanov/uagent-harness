package sandbox_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/sandbox"
)

func TestPolicy_CanWrite(t *testing.T) {
	ws := t.TempDir()
	extra := t.TempDir()
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	outside := filepath.Join(home, "uah-canwrite-never-created", "x.txt")
	link := filepath.Join(ws, "out")
	require.NoError(t, os.Symlink(filepath.Dir(outside), link))

	p := sandbox.Policy{Mode: sandbox.WorkspaceWrite, Workspace: ws, WritableRoots: []string{extra}}
	assert.True(t, p.CanWrite(filepath.Join(ws, "a/b.txt")), "a new file in the workspace")
	assert.True(t, p.CanWrite(filepath.Join(extra, "c.txt")), "a writable root")
	assert.False(t, p.CanWrite(outside), "outside every root")
	assert.False(t, p.CanWrite(filepath.Join(link, "x.txt")), "a symlink out of the workspace")
	for _, name := range sandbox.ProtectedNames {
		assert.False(t, p.CanWrite(filepath.Join(ws, name, "config")), name+" stays protected")
	}

	p.Mode = sandbox.ReadOnly
	assert.False(t, p.CanWrite(filepath.Join(ws, "a.txt")))
	p.Mode = sandbox.FullAccess
	assert.True(t, p.CanWrite(outside))
}
