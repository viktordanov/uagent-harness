package main_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

// TestConfigCommandLayers checks that `uah config` names the layer that set
// each value, in merge order.
func TestConfigCommandLayers(t *testing.T) {
	root, ws, _, env := configEnv(t)
	layer := filepath.Join(root, "home", "config.d", "10-host.toml")
	writeFile(t, layer, "model = \"gpt-6-sol\"\n[approvals]\nallow = [\"host\"]\n[tui]\ntitle = false\n")
	extra := filepath.Join(root, "extra.toml")
	writeFile(t, extra, "[[hooks.Stop]]\ncommand = \"extra\"\n")
	env = append(env, "UAH_EXTRA_CONFIG="+extra)

	res := uahWith(t, env, "", "config", "--json", "-C", ws)

	require.Equal(t, 0, res.code, res.stderr)
	var rep struct {
		Layers []struct {
			Path, State string
		} `json:"layers"`
		Settings []struct {
			Key     string   `json:"key"`
			Value   any      `json:"value"`
			Sources []string `json:"sources"`
		} `json:"settings"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.stdout), &rep))
	require.Len(t, rep.Layers, 2)
	assert.Equal(t, []string{layer, extra}, []string{rep.Layers[0].Path, rep.Layers[1].Path})
	byKey := map[string][]string{}
	values := map[string]any{}
	for _, s := range rep.Settings {
		byKey[s.Key], values[s.Key] = s.Sources, s.Value
	}
	assert.Equal(t, []string{"config.d/10-host.toml"}, byKey["model"])
	assert.Equal(t, []string{"user file", "config.d/10-host.toml", "project file"}, byKey["approvals.allow"])
	assert.Equal(t, []string{"user file", "UAH_EXTRA_CONFIG"}, byKey["hooks.Stop"])
	assert.Equal(t, []string{"config.d/10-host.toml"}, byKey["tui.title"])
	assert.Equal(t, false, values["tui.title"])

	res = uahWith(t, env, "", "config", "-C", ws)
	require.Equal(t, 0, res.code, res.stderr)
	assert.Contains(t, res.stdout, "layer:        "+layer+" (read)\nlayer:        "+extra+" (read)\n")

	t.Run("a missing UAH_EXTRA_CONFIG file is a usage error", func(t *testing.T) {
		res := uahWith(t, append(env, "UAH_EXTRA_CONFIG="+filepath.Join(root, "none.toml")), "", "config", "-C", ws)

		assert.Equal(t, 2, res.code)
		assert.Contains(t, res.stderr, "UAH_EXTRA_CONFIG names")
	})
}

// TestLayerHooksRun runs a session whose hooks come from config.d and
// UAH_EXTRA_CONFIG: they run as written, with no trust step, and `uah
// hooks` and `uah doctor` list them.
func TestLayerHooksRun(t *testing.T) {
	e, env := fakeEnv(t, fakellm.Reply{Text: "done"})
	homeDir := filepath.Join(e.StateDir, "..", "home")
	out := filepath.Join(t.TempDir(), "events.jsonl")
	record := "cat >> " + out + " && echo >> " + out
	writeFile(t, filepath.Join(homeDir, "config.d", "host.toml"),
		"[[hooks.SessionStart]]\ncommand = \""+record+"\"\n[[hooks.Stop]]\ncommand = \""+record+"\"\n")
	extra := filepath.Join(t.TempDir(), "extra.toml")
	writeFile(t, extra, "[[hooks.UserPromptSubmit]]\ncommand = \""+record+"\"\n")
	env = append(env, "UAH_EXTRA_CONFIG="+extra)

	res := uahWith(t, env, "", "run", "-C", e.Workspace, "hi")

	require.Equal(t, 0, res.code, res.stderr)
	data, err := os.ReadFile(out)
	require.NoError(t, err)
	var events []string
	for line := range strings.Lines(strings.TrimSpace(string(data))) {
		var in struct {
			Event string `json:"hook_event_name"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &in), line)
		events = append(events, in.Event)
	}
	assert.Equal(t, []string{"SessionStart", "UserPromptSubmit", "Stop"}, events)

	res = uahWith(t, env, "", "hooks", "-C", e.Workspace)
	require.Equal(t, 0, res.code, res.stderr)
	assert.Regexp(t, `SessionStart\s+config.d/host.toml\s+runs`, res.stdout)
	assert.Regexp(t, `UserPromptSubmit\s+UAH_EXTRA_CONFIG\s+runs`, res.stdout)

	res = uahWith(t, env, "", "doctor", "-C", e.Workspace)
	assert.Contains(t, res.stdout, "3 configured: 0 user, 2 config.d/host.toml, 1 UAH_EXTRA_CONFIG, 0 project (0 trusted)")
}
