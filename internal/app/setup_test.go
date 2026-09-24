package app_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/app"
	"github.com/viktordanov/uagent-harness/testing/harnesstest"
)

// setupEnv isolates the configuration directory and CODEX_HOME, and returns
// inputs for a new session in a fresh workspace.
func setupEnv(t *testing.T) (*harnesstest.Env, app.Inputs) {
	t.Helper()
	e := harnesstest.NewEnv(t)
	configDir := filepath.Join(e.StateDir, "..", "config")
	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv("CODEX_HOME", e.CodexHome)

	return e, app.Inputs{
		ConfigPath: filepath.Join(configDir, "uagent", "config.toml"),
		StateDir:   e.StateDir,
		Workspace:  e.Workspace,
		LogLevel:   "warn",
		Timeout:    30 * time.Minute,
		MaxDisk:    "5G",
	}
}

func TestSetup(t *testing.T) {
	e, in := setupEnv(t)
	require.NoError(t, os.WriteFile(filepath.Join(e.Workspace, "AGENTS.md"), []byte("Use tabs in Go files."), 0o600))

	res, err := app.Setup(in, io.Discard)

	require.NoError(t, err)
	assert.Equal(t, e.StateDir, res.StateDir)
	assert.Equal(t, app.EngineEmbedded, res.Engine.Name())
	assert.Equal(t, filepath.Join(e.StateDir, "sessions"), res.Options.SessionsDir)
	assert.False(t, res.Options.Resumed)
	assert.Equal(t, app.CodexProvider, res.Options.Settings.Provider)
	require.NotNil(t, res.Options.Instructions)
	assert.Equal(t, []string{filepath.Join(e.Workspace, "AGENTS.md")}, res.Options.Instructions.Files)
	assert.Contains(t, res.Options.Settings.SystemPrompt, "Use tabs in Go files.")
	assert.Nil(t, res.Options.Hooks)

	t.Run("without instructions", func(t *testing.T) {
		in := in
		in.NoInstructions = true

		res, err := app.Setup(in, io.Discard)

		require.NoError(t, err)
		assert.Nil(t, res.Options.Instructions)
		assert.Empty(t, res.Options.Settings.SystemPrompt)
	})
}

func TestSetupUsageErrors(t *testing.T) {
	tests := []struct {
		name   string
		in     func(*app.Inputs)
		config string
		want   string
	}{
		{name: "an unknown session", in: func(in *app.Inputs) { in.SessionRef = "zzzz" }, want: `no session matches "zzzz"`},
		{name: "a config typo", config: "efort = \"low\"\n", want: `unknown key "efort"`},
		{
			name: "fast on a provider without priority processing",
			in:   func(in *app.Inputs) { in.Provider, in.Fast, in.FastSet = "ollama", true, true },
			want: "--fast needs the openai or openai-codex provider",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, in := setupEnv(t)
			if tt.in != nil {
				tt.in(&in)
			}
			if tt.config != "" {
				require.NoError(t, os.MkdirAll(filepath.Dir(in.ConfigPath), 0o700))
				require.NoError(t, os.WriteFile(in.ConfigPath, []byte(tt.config), 0o600))
			}

			_, err := app.Setup(in, io.Discard)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
			var usage *app.UsageError
			assert.True(t, errors.As(err, &usage), "a usage error")
		})
	}
}
