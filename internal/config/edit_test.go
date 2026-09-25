package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/config"
)

func TestSetValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("# mine\nmodel = \"gpt-6-sol\"\n\n[tui]\ndetails = true # start detailed\n"), 0o640))

	require.NoError(t, config.SetValue(path, "tui.mouse", true))
	require.NoError(t, config.SetValue(path, "auto_compact_percent", 80))
	require.NoError(t, config.SetValue(path, "model", nil))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "# mine\nauto_compact_percent = 80\n\n[tui]\ndetails = true # start detailed\nmouse = true\n", string(data))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm(), "the file keeps its permissions")

	cfg, _, err := config.Load(path, t.TempDir())
	require.NoError(t, err)
	assert.True(t, cfg.TUI.MouseOn())
	assert.Equal(t, 80, *cfg.AutoCompactPercent)
}

func TestSetValueKeepsTheFileOnABadEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	before := []byte("fast = true\n")
	require.NoError(t, os.WriteFile(path, before, 0o600))

	require.ErrorContains(t, config.SetValue(path, "auto_compact_percent", "high"), "the edit would break the file")
	require.ErrorContains(t, config.SetValue(path, "no_such_key", 1), "unknown key")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, data)
}
