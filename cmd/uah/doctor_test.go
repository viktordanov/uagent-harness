package main_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoctor(t *testing.T) {
	e, env := fakeEnv(t, "simple.jsonl")
	env = append(env, "UNREAL_HARNESS_LLM_BASE_URL="+modelsServer(t).URL)

	res := uahWith(t, env, "", "doctor", "-C", e.Workspace)
	require.Equal(t, 0, res.code, res.stdout+res.stderr)
	for _, line := range []string{
		"✓ config: no configuration files", "✓ runner: ", "✓ credentials: openai-codex",
		"✓ models: 2 models available to this login (live list from openai-codex); gpt-6-sol is in it",
		"✓ usage: pro · weekly 78% left (resets ", "✓ hooks: none configured", "✓ state: ",
	} {
		assert.Contains(t, "\n"+res.stdout, "\n"+line, "stdout:\n%s", res.stdout)
	}
	assert.NotContains(t, res.stdout, "✗")

	t.Run("broken", func(t *testing.T) {
		config := filepath.Join(e.StateDir, "..", "config", "uagent", "config.toml")
		require.NoError(t, os.MkdirAll(filepath.Dir(config), 0o700))
		require.NoError(t, os.WriteFile(config, []byte("model = \n"), 0o600))
		broken := append(env, "UAGENT_RUNNER=/nonexistent/unreal-agent-runner")

		res := uahWith(t, broken, "", "doctor", "-C", e.Workspace)
		assert.Equal(t, 1, res.code)
		assert.Regexp(t, `(?m)^✗ config: .*config\.toml`, res.stdout)
		assert.Regexp(t, `(?m)^✗ runner: .*nonexistent`, res.stdout)
		assert.Regexp(t, `(?m)^    fix: install unreal-agent-runner`, res.stdout)
		assert.Empty(t, res.stderr, "the checks say it all")

		res = uahWith(t, broken, "", "doctor", "--json", "-C", e.Workspace)
		assert.Equal(t, 1, res.code)
		var out struct {
			OK     bool `json:"ok"`
			Checks []struct {
				Name, Status, Detail, Fix string
			} `json:"checks"`
		}
		require.NoError(t, json.Unmarshal([]byte(res.stdout), &out), res.stdout)
		assert.False(t, out.OK)
		require.NotEmpty(t, out.Checks)
		assert.Equal(t, "config", out.Checks[0].Name)
		assert.Equal(t, "fail", out.Checks[0].Status)
		assert.NotEmpty(t, out.Checks[0].Fix)
	})
}
