package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/config"
)

// TestLoad_ModelInstructionsFile resolves model_instructions_file as Codex
// resolves an AbsolutePathBuf: relative to the directory of the file that
// set it, ~/ under the home directory, an absolute path as it is. The
// project file's value wins.
func TestLoad_ModelInstructionsFile(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "ws")
	user := filepath.Join(root, "home", "config.toml")
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	tests := []struct {
		value, want string
	}{
		{value: "prompts/system.md", want: filepath.Join(root, "home", "prompts", "system.md")},
		{value: "../shared/system.md", want: filepath.Join(root, "shared", "system.md")},
		{value: "~/prompts/system.md", want: filepath.Join(home, "prompts", "system.md")},
		{value: "/etc/uah/system.md", want: "/etc/uah/system.md"},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			write(t, user, "model_instructions_file = \""+tt.value+"\"\n")

			cfg, _, err := config.Load(user, ws)

			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.ModelInstructionsFile)
		})
	}

	t.Run("a trusted project file wins, relative to itself", func(t *testing.T) {
		write(t, user, "model_instructions_file = \"system.md\"\n[projects.\""+ws+"\"]\ntrusted = true\n")
		write(t, config.ProjectFile(ws), "model_instructions_file = \"mine.md\"\n")

		l, err := config.LoadLayers(user, ws)

		require.NoError(t, err)
		assert.Equal(t, filepath.Join(root, "home", "system.md"), l.User.ModelInstructionsFile)
		assert.Equal(t, filepath.Join(ws, ".uah", "mine.md"), l.Merged().ModelInstructionsFile)
	})
}
