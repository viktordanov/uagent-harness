package session_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/session"
)

func TestInDir(t *testing.T) {
	root := t.TempDir()
	proj, other := filepath.Join(root, "proj"), filepath.Join(root, "other")
	require.NoError(t, os.MkdirAll(filepath.Join(proj, "sub"), 0o700))
	require.NoError(t, os.MkdirAll(other, 0o700))
	link := filepath.Join(root, "link")
	require.NoError(t, os.Symlink(proj, link))
	infos := []session.Info{
		{ID: "a", Workspace: proj},
		{ID: "b", Workspace: filepath.Join(proj, "sub")},
		{ID: "c", Workspace: other},
		{ID: "d", Workspace: link},
		{ID: "e", Workspace: proj + "/./"},
	}

	got := session.InDir(infos, proj)

	ids := make([]string, 0, len(got))
	for _, in := range got {
		ids = append(ids, in.ID)
	}
	assert.Equal(t, []string{"a", "d", "e"}, ids, "exact directory after normalization; subdirectories do not match")
	assert.True(t, session.SameDir(link, proj+"/"))
}
