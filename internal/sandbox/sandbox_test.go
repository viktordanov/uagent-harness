package sandbox_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/internal/sandbox"
)

func TestWritable(t *testing.T) {
	ws, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	extra := filepath.Join(ws, "cache")
	t.Setenv("TMPDIR", "")

	assert.Empty(t, sandbox.Policy{Mode: sandbox.ReadOnly, Workspace: ws}.Writable())
	got := sandbox.Policy{Mode: sandbox.WorkspaceWrite, Workspace: ws, WritableRoots: []string{extra, ws}}.Writable()
	tmp, _ := filepath.EvalSymlinks("/tmp")
	assert.Equal(t, []string{ws, extra, tmp}, got)

	temp := filepath.Join(ws, "session-tmp")
	assert.Equal(t, []string{temp}, sandbox.Policy{Mode: sandbox.ReadOnly, Workspace: ws, TempDir: temp}.Writable(),
		"read-only writes only the temp dir")
	got = sandbox.Policy{Mode: sandbox.WorkspaceWrite, Workspace: ws, TempDir: temp}.Writable()
	assert.Equal(t, []string{ws, tmp, temp}, got)
	assert.Empty(t, sandbox.Policy{Mode: sandbox.FullAccess, Workspace: ws, TempDir: temp}.Writable(), "no sandbox, no roots")
}

func TestProtected(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: ../main/.git/worktrees/x\n"), 0o600))
	got := sandbox.Protected(root)
	assert.Contains(t, got, filepath.Join(root, ".uah"))
	assert.Contains(t, got, filepath.Join(root, ".uagent"), "the old project directory until it is moved")
	assert.Contains(t, got, filepath.Join(filepath.Dir(root), "main", ".git", "worktrees", "x"))
}

func TestParseMode(t *testing.T) {
	m, err := sandbox.ParseMode("workspace-write")
	require.NoError(t, err)
	assert.Equal(t, sandbox.WorkspaceWrite, m)
	_, err = sandbox.ParseMode("yolo")
	require.Error(t, err)
}
