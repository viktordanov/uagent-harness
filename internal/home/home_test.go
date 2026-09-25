package home_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent-harness/internal/home"
)

func TestDir(t *testing.T) {
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(userHome, "xdg"))
	t.Setenv(home.Env, "")
	assert.Equal(t, filepath.Join(userHome, ".uah"), home.Dir(), "XDG_CONFIG_HOME does not apply")

	custom := filepath.Join(t.TempDir(), "elsewhere")
	t.Setenv(home.Env, custom+"/")
	assert.Equal(t, custom, home.Dir(), "UAH_HOME wins")
}

func TestWarnings(t *testing.T) {
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }
	assert.Empty(t, home.Warnings(getenv))

	env["UAGENT_CONFIG"] = "/x/config.toml"
	env["UAGENT_STATE_DIR"] = "/x/state"
	assert.Equal(t, []string{
		"UAGENT_CONFIG is no longer read; set UAH_CONFIG instead",
		"UAGENT_STATE_DIR is no longer read; set UAH_STATE_DIR instead",
	}, home.Warnings(getenv))
}
