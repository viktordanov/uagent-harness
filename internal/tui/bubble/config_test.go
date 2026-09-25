package bubble_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/app"
	"github.com/viktordanov/uagent-harness/internal/tui/bubble"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// configDeps back /config with a real user file, as `uah` does.
func configDeps(t *testing.T, d *withUserFile) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("UAH_HOME", dir)
	in := app.Inputs{
		ConfigPath: filepath.Join(dir, "config.toml"), StateDir: t.TempDir(), Workspace: t.TempDir(),
		Timeout: 30 * time.Minute, MaxDisk: "5G",
	}
	d.path = in.ConfigPath
	d.Config = func(ctx context.Context) state.ConfigLoaded {
		rep, err := app.Inspect(ctx, in)
		if err != nil {
			return state.ConfigLoaded{Err: err}
		}
		values := map[string]state.ConfigValue{}
		for _, s := range rep.Settings {
			values[s.Key] = state.ConfigValue{Value: s.Text(), Source: s.SourceText()}
		}

		return state.ConfigLoaded{Path: rep.UserFile.Path, Values: values}
	}
	d.SaveConfig = func(key string, value any) error { return app.SaveSetting(context.Background(), in, key, value) }
}

func TestTUI_ConfigSavesToTheUserFile(t *testing.T) {
	d := &withUserFile{Deps: deps(t, "simple.jsonl")}
	configDeps(t, d)
	require.NoError(t, os.MkdirAll(filepath.Dir(d.path), 0o700))
	require.NoError(t, os.WriteFile(d.path, []byte("# my settings\neffort = \"high\" # keep\n"), 0o600))
	dr := start(t, d.Deps)
	dr.typeText("hi")
	dr.key(tea.KeyEnter, 0)
	dr.waitFor("• hello")

	dr.typeText("/config")
	dr.key(tea.KeyEnter, 0)
	dr.waitFor("Mouse")
	assert.Regexp(t, `› Auto-compact +on at 90% +default`, dr.view())
	assert.Regexp(t, `Effort +high +user file`, dr.view())

	dr.key(tea.KeyUp, 0) // wraps to Mouse
	dr.key(tea.KeySpace, 0)
	dr.waitFor("saved tui.mouse = true")
	assert.Equal(t, tea.MouseModeCellMotion, dr.m.View().MouseMode, "the mouse is on at once")

	dr.key(tea.KeyDown, 0)
	dr.key(tea.KeyDown, 0) // the token limit
	dr.key(tea.KeyEnter, 0)
	dr.typeText("50000")
	dr.key(tea.KeyEnter, 0)
	dr.waitFor("saved model_auto_compact_token_limit = 50000")
	dr.waitFor("50000")

	data, err := os.ReadFile(d.path)
	require.NoError(t, err)
	assert.Equal(t, "# my settings\neffort = \"high\" # keep\nmodel_auto_compact_token_limit = 50000\n\n[tui]\nmouse = true\n", string(data))

	dr.key(tea.KeyEscape, 0)
	assert.NotContains(t, dr.view(), "enter or space change", "esc closes the panel")
}

func TestTUI_ConfigUndoesAChangeThatBreaksTheStart(t *testing.T) {
	d := &withUserFile{Deps: deps(t, "simple.jsonl")}
	configDeps(t, d)
	require.NoError(t, os.MkdirAll(filepath.Dir(d.path), 0o700))
	before := []byte("engine = \"process\"\n")
	require.NoError(t, os.WriteFile(d.path, before, 0o600))

	err := d.SaveConfig("fast", true)
	require.ErrorContains(t, err, "not saved: --fast needs the embedded engine")
	data, rerr := os.ReadFile(d.path)
	require.NoError(t, rerr)
	assert.Equal(t, before, data)
}

// withUserFile is the TUI's deps and the user file /config saves to.
type withUserFile struct {
	bubble.Deps

	path string
}
