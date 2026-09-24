package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/config"
	"github.com/viktordanov/uagent-harness/internal/hooks"
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

	t.Run("compaction keys, and the project file overrides them", func(t *testing.T) {
		root := t.TempDir()
		ws := filepath.Join(root, "ws")
		user := filepath.Join(root, "config.toml")
		write(t, user, "auto_compact_percent = 80\nmodel_context_window = 128000\n[projects.\""+ws+"\"]\ntrusted = true\n")
		write(t, config.ProjectFile(ws), "auto_compact_percent = 0\n")

		cfg, _, err := config.Load(user, ws)

		require.NoError(t, err)
		require.NotNil(t, cfg.AutoCompactPercent)
		assert.Equal(t, 0, *cfg.AutoCompactPercent, "0 in the project file turns it off")
		assert.Equal(t, int64(128000), cfg.ModelContextWindow)
	})

	t.Run("reviewer keys, and the project file overrides them", func(t *testing.T) {
		root := t.TempDir()
		ws := filepath.Join(root, "ws")
		user := filepath.Join(root, "config.toml")
		write(t, user, "approvals_reviewer = \"user\"\n[review]\nmodel = \"m\"\neffort = \"medium\"\ntimeout = \"30s\"\n[projects.\""+ws+"\"]\ntrusted = true\n")
		write(t, config.ProjectFile(ws), "[review]\neffort = \"low\"\n")

		cfg, _, err := config.Load(user, ws)

		require.NoError(t, err)
		assert.Equal(t, "user", cfg.ApprovalsReviewer)
		assert.Equal(t, config.Review{Model: "m", Effort: "low", Timeout: "30s"}, cfg.Review)
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

func TestHooks(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "ws")
	user := filepath.Join(root, "config.toml")
	write(t, user, `
[projects."`+ws+`"]
trusted = true

[[hooks.PreToolUse]]
matcher = "Bash"
command = "check.sh"
timeout = "5s"
`)
	write(t, config.ProjectFile(ws), "[[hooks.Stop]]\ncommand = \"notify.sh\"\n")

	cfg, _, err := config.Load(user, ws)
	require.NoError(t, err)
	list, err := cfg.HookList()
	require.NoError(t, err)
	assert.Equal(t, []hooks.Hook{
		{Event: hooks.PreToolUse, Matcher: "Bash", Command: "check.sh", Timeout: 5 * time.Second, Source: hooks.SourceUser},
		{Event: hooks.Stop, Command: "notify.sh", Source: hooks.SourceProject},
	}, list)

	write(t, user, "[[hooks.Nope]]\ncommand = \"x\"\n")
	cfg, _, err = config.Load(user, ws)
	require.NoError(t, err)
	_, err = cfg.HookList()
	require.ErrorContains(t, err, `unknown hook event "Nope"`)
}
