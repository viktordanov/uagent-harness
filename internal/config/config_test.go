package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/config"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func TestLoad(t *testing.T) {
	t.Run("missing files are an empty config", func(t *testing.T) {
		cfg, loaded, err := config.Load(filepath.Join(t.TempDir(), "none.toml"), t.TempDir())

		require.NoError(t, err)
		assert.Empty(t, loaded)
		assert.True(t, cfg.InstructionsEnabled())
	})

	t.Run("a project file applies only when the user file trusts the workspace", func(t *testing.T) {
		root := t.TempDir()
		ws := filepath.Join(root, "ws")
		user := filepath.Join(root, "config.toml")
		write(t, config.ProjectFile(ws), "effort = \"max\"\n[instructions]\nenabled = false\n")
		write(t, user, "provider = \"openrouter\"\neffort = \"low\"\ntimeout = \"10m\"\n")

		untrusted, loaded, err := config.Load(user, ws)
		require.NoError(t, err)
		assert.Equal(t, "low", untrusted.Effort)
		assert.Equal(t, []string{user}, loaded)

		write(t, user, "provider = \"openrouter\"\neffort = \"low\"\ntimeout = \"10m\"\n[projects.\""+ws+"\"]\ntrusted = true\n")
		trusted, loaded, err := config.Load(user, ws)
		require.NoError(t, err)
		assert.Equal(t, "max", trusted.Effort, "the project file overrides the user file")
		assert.Equal(t, "openrouter", trusted.Provider, "unset project values keep the user's")
		assert.False(t, trusted.InstructionsEnabled())
		assert.Equal(t, []string{user, config.ProjectFile(ws)}, loaded)
		d, ok, err := trusted.TimeoutValue()
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, 10*time.Minute, d)
	})

	t.Run("unknown keys are errors", func(t *testing.T) {
		user := filepath.Join(t.TempDir(), "config.toml")
		write(t, user, "efort = \"low\"\n")

		_, _, err := config.Load(user, t.TempDir())

		require.ErrorContains(t, err, `unknown key "efort"`)
	})

	t.Run("a project file cannot trust itself", func(t *testing.T) {
		root := t.TempDir()
		ws := filepath.Join(root, "ws")
		user := filepath.Join(root, "config.toml")
		write(t, user, "[projects.\""+ws+"\"]\ntrusted = true\n")
		write(t, config.ProjectFile(ws), "[projects.\"/elsewhere\"]\ntrusted = true\n")

		_, _, err := config.Load(user, ws)

		require.ErrorContains(t, err, "user file only")
	})
}
