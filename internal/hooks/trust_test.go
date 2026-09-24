package hooks_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/hooks"
)

// TestTrust_Scripts: a command that runs a local script is trusted with the
// script's content, so editing the script needs trust again.
func TestTrust_Scripts(t *testing.T) {
	ws := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(ws, ".uagent", "hooks"), 0o700))
	script := filepath.Join(ws, ".uagent", "hooks", "stop.sh")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\nexit 2\n"), 0o700))

	commands := map[string]string{
		"relative":         ".uagent/hooks/stop.sh --flag",
		"project variable": `"$UAH_PROJECT_DIR"/.uagent/hooks/stop.sh`,
		"absolute":         script + " && echo done",
	}
	for name, command := range commands {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "trust.json")
			trust, err := hooks.LoadTrust(path)
			require.NoError(t, err)
			ok, why := trust.Check(ws, command)
			assert.False(t, ok)
			assert.Equal(t, hooks.ReasonUntrusted, why)

			require.NoError(t, trust.Allow(ws, command))
			reloaded, err := hooks.LoadTrust(path)
			require.NoError(t, err)
			assert.True(t, reloaded.Trusted(ws, command))
			assert.False(t, reloaded.Trusted(t.TempDir(), ".uagent/hooks/stop.sh --flag"), "another workspace's script is not trusted")

			require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o700))
			t.Cleanup(func() { _ = os.WriteFile(script, []byte("#!/bin/sh\nexit 2\n"), 0o700) })
			ok, why = reloaded.Check(ws, command)
			assert.False(t, ok, "a changed script needs trust again")
			assert.Equal(t, hooks.ReasonScriptChanged, why)
		})
	}
}

func TestTrust_CommandsWithoutAScript(t *testing.T) {
	ws := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(ws, "true"), []byte("exit 1"), 0o700))
	trust, err := hooks.LoadTrust(filepath.Join(t.TempDir(), "trust.json"))
	require.NoError(t, err)
	for _, command := range []string{"true", "./missing.sh", "$(pick)/x.sh", "echo 'a/b'"} {
		require.NoError(t, trust.Allow(ws, command))
		assert.True(t, trust.Trusted(ws, command), "%q runs no local script", command)
		assert.True(t, trust.Trusted(t.TempDir(), command), "%q is trusted by its text alone", command)
	}
}

// TestTrust_OldEntries: entries without a script hash, from before scripts
// were hashed, still trust plain commands, but a command that runs a script
// needs trust again.
func TestTrust_OldEntries(t *testing.T) {
	ws := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(ws, "check.sh"), []byte("exit 0"), 0o700))
	old := map[string]map[string]string{}
	for _, c := range []string{"echo hi", "./check.sh"} {
		old[sha(c)] = map[string]string{"command": c, "workspace": ws, "trusted_at": "2026-01-02T03:04:05Z"}
	}
	data, err := json.Marshal(old)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "trust.json")
	require.NoError(t, os.WriteFile(path, data, 0o600))

	trust, err := hooks.LoadTrust(path)
	require.NoError(t, err)
	assert.True(t, trust.Trusted(ws, "echo hi"))
	ok, why := trust.Check(ws, "./check.sh")
	assert.False(t, ok)
	assert.Equal(t, hooks.ReasonScriptNew, why)
	require.NoError(t, trust.Allow(ws, "./check.sh"))
	assert.True(t, trust.Trusted(ws, "./check.sh"))
}

// TestRun_ReportsAChangedScript: the runner skips a project hook whose
// script changed and says why.
func TestRun_ReportsAChangedScript(t *testing.T) {
	ws := t.TempDir()
	script := filepath.Join(ws, "stop.sh")
	require.NoError(t, os.WriteFile(script, []byte("exit 2\n"), 0o700))
	trust, err := hooks.LoadTrust(filepath.Join(t.TempDir(), "trust.json"))
	require.NoError(t, err)
	project := hooks.Hook{Event: hooks.Stop, Command: "./stop.sh", Source: hooks.SourceProject}
	require.NoError(t, trust.Allow(ws, project.Command))
	r, err := hooks.New([]hooks.Hook{project}, trust, ws)
	require.NoError(t, err)
	assert.True(t, r.Run(context.Background(), hooks.Input{Event: hooks.Stop}).Block, "the trusted script runs")

	require.NoError(t, os.WriteFile(script, []byte("exit 0 # edited\n"), 0o700))
	var got []hooks.Result
	r.OnResult(func(res hooks.Result) { got = append(got, res) })
	assert.False(t, r.Run(context.Background(), hooks.Input{Event: hooks.Stop}).Block)
	require.Len(t, got, 1)
	assert.Equal(t, hooks.OutcomeSkipped, got[0].Outcome)
	assert.Equal(t, hooks.ReasonScriptChanged, got[0].Reason)
}

func sha(s string) string {
	sum := sha256.Sum256([]byte(s))

	return hex.EncodeToString(sum[:])
}
