package sandbox_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/sandbox"
)

func TestShellFullAccessIsTheRealShell(t *testing.T) {
	got, err := sandbox.Shell(t.TempDir(), sandbox.Policy{Mode: sandbox.FullAccess}, "/bin/zsh")
	require.NoError(t, err)
	assert.Equal(t, "/bin/zsh", got)
}

func TestShellScript(t *testing.T) {
	p := sandbox.Policy{Mode: sandbox.WorkspaceWrite, Workspace: t.TempDir()}
	if _, err := p.Wrap([]string{"/bin/sh"}); err != nil {
		t.Skipf("no sandbox here: %v", err)
	}
	dir := t.TempDir()
	first, err := sandbox.Shell(dir, p, "/bin/sh")
	require.NoError(t, err)
	again, err := sandbox.Shell(dir, p, "/bin/sh")
	require.NoError(t, err)
	assert.Equal(t, first, again, "the same policy reuses its script")
	info, err := os.Stat(first)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temporary files are left")
	assert.Equal(t, filepath.Dir(first), dir)
}
