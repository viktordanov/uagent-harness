package main_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPrompts writes the built-in prompts beside the configuration file,
// refuses to overwrite them without --force, prints the keys that use
// them, and prints one prompt; the written keys load.
func TestPrompts(t *testing.T) {
	user, env := mcpEnv(t)
	dir := filepath.Join(filepath.Dir(user), "prompts")

	res := uahWith(t, env, "", "prompts", "init")
	require.Equal(t, 0, res.code, res.stderr)
	compact := filepath.Join(dir, "compact.md")
	review := filepath.Join(dir, "review.md")
	assert.Equal(t, "Wrote "+compact+"\nWrote "+review+"\n\nTo use them, add to "+user+":\n\n"+
		"experimental_compact_prompt_file = \""+compact+"\"\n\n[review]\npolicy_file = \""+review+"\"\n", res.stdout)
	data, err := os.ReadFile(review)
	require.NoError(t, err)
	shown := uahWith(t, env, "", "prompts", "show", "review")
	require.Equal(t, 0, shown.code, shown.stderr)
	assert.Equal(t, string(data), shown.stdout)
	assert.Contains(t, shown.stdout, "risk")
	shown = uahWith(t, env, "", "prompts", "show", "compact")
	assert.Contains(t, shown.stdout, "CONTEXT CHECKPOINT COMPACTION")

	require.NoError(t, os.WriteFile(review, []byte("mine\n"), 0o600))
	res = uahWith(t, env, "", "prompts", "init")
	assert.Equal(t, 2, res.code)
	assert.Contains(t, res.stderr, "exists; use --force")
	data, err = os.ReadFile(review)
	require.NoError(t, err)
	assert.Equal(t, "mine\n", string(data), "not overwritten")
	res = uahWith(t, env, "", "prompts", "init", "--force")
	require.Equal(t, 0, res.code, res.stderr)

	f, err := os.OpenFile(user, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString("experimental_compact_prompt_file = \"" + compact + "\"\n\n[review]\npolicy_file = \"" + review + "\"\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	res = uahWith(t, env, "", "config")
	require.Equal(t, 0, res.code, res.stderr)
	assert.Contains(t, res.stdout, review)

	res = uahWith(t, env, "", "prompts", "show", "other")
	assert.Equal(t, 2, res.code)
}
