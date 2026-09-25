package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/config"
)

func TestProjectMove(t *testing.T) {
	repo := t.TempDir()
	ws := filepath.Join(repo, "sub")
	require.NoError(t, os.MkdirAll(filepath.Join(ws, ".uagent"), 0o700))
	assert.Equal(t, "mv .uagent .uah", config.ProjectMove(ws), "not a repository")

	require.NoError(t, os.Mkdir(filepath.Join(repo, ".git"), 0o700))
	assert.Equal(t, "git mv .uagent .uah", config.ProjectMove(ws), "a parent is the repository")
	assert.Equal(t, filepath.Join(ws, ".uagent")+" is no longer read; uah reads project files from .uah now: run `git mv .uagent .uah` in "+ws,
		config.ProjectMoveNotice(ws))

	require.NoError(t, os.Mkdir(filepath.Join(ws, ".uah"), 0o700))
	assert.Empty(t, config.ProjectMove(ws), "already moved")
	assert.Empty(t, config.ProjectMoveNotice(t.TempDir()), "nothing to move")
}
