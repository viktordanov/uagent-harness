package main_test

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite golden files")

// configEnv writes a user file that trusts a workspace with a project file,
// and returns the root, the workspace, and the environment for uah.
func configEnv(t *testing.T) (root, ws, user string, env []string) {
	t.Helper()
	root = t.TempDir()
	ws = filepath.Join(root, "ws")
	user = filepath.Join(root, "home", "config.toml")
	writeFile(t, user, `model = "gpt-6-luna"
effort = "low"
fast = true

[approvals]
allow = ["go test"]

[mcp_servers.docs]
command = "docs-mcp"

[[hooks.Stop]]
command = "notify"

[projects."`+ws+`"]
trusted = true
`)
	writeFile(t, filepath.Join(ws, ".uah", "config.toml"), `effort = "medium"
auto_compact_percent = 80

[approvals]
allow = ["make"]

[mcp_servers.docs]
url = "https://docs.example.com/mcp"
`)

	return root, ws, user, []string{
		"UAH_HOME=" + filepath.Join(root, "home"),
		"UAH_CONFIG=" + user, "UNREAL_HARNESS_LLM_PROVIDER=", "UNREAL_HARNESS_LLM_MODEL=",
		"UAH_ENGINE=", "UAH_ASK=", "UAH_SANDBOX=read-only",
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func TestConfigCommand(t *testing.T) {
	root, ws, _, env := configEnv(t)

	res := uahWith(t, env, "", "config", "-C", ws, "--timeout", "10m")

	require.Equal(t, 0, res.code, res.stderr)
	got := strings.ReplaceAll(res.stdout, root, "$ROOT")
	path := filepath.Join("testdata", "config.golden")
	if *update {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o600))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(want), got)
}

func TestConfigCommandJSON(t *testing.T) {
	_, ws, _, env := configEnv(t)

	res := uahWith(t, env, "", "config", "--json", "-C", ws, "--provider", "openai")

	require.Equal(t, 0, res.code, res.stderr)
	var rep struct {
		Workspace string `json:"workspace"`
		Settings  []struct {
			Key     string   `json:"key"`
			Value   any      `json:"value"`
			Sources []string `json:"sources"`
		} `json:"settings"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.stdout), &rep))
	assert.Equal(t, ws, rep.Workspace)
	byKey := map[string][]string{}
	values := map[string]any{}
	for _, s := range rep.Settings {
		byKey[s.Key], values[s.Key] = s.Sources, s.Value
	}
	assert.Equal(t, []string{"flag"}, byKey["provider"])
	assert.Equal(t, []string{"default"}, byKey["model"], "a provider flag drops the configured model")
	assert.Equal(t, []string{"user file", "project file"}, byKey["approvals.allow"])
	assert.Equal(t, []any{"go test", "make"}, values["approvals.allow"])
	assert.Equal(t, 80.0, values["auto_compact_percent"])
}

func TestConfigCommandBadConfig(t *testing.T) {
	root, ws, user, env := configEnv(t)
	other := filepath.Join(root, "other.toml")
	writeFile(t, other, "no_such_key = 1\n")

	res := uahWith(t, env, "", "config", "-C", ws, "--config", other)
	assert.Equal(t, 2, res.code)
	assert.Contains(t, res.stderr, "unknown key")

	writeFile(t, user, "no_such_key = 1\n")
	res = uahWith(t, env, "", "config", "-C", ws)

	assert.Equal(t, 2, res.code)
	assert.Contains(t, res.stderr, "unknown key")
}
