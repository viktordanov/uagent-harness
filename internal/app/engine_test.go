package app_test

import (
	"context"
	"io"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/app"
)

// TestEngine_RemovedSettingWarns: engine in a configuration file,
// UAH_ENGINE, or --engine, whatever its value, no longer fails; the
// session opens on the embedded engine with one warning, and `uah doctor`
// shows the same warning.
func TestEngine_RemovedSettingWarns(t *testing.T) {
	const removed = "the process engine was removed in uah 1.2; uah always uses the embedded engine"
	cases := map[string]struct {
		config, flag string
		where        string
	}{
		"nothing":           {},
		"the config file":   {config: "engine = \"process\"\n", where: "(drop engine from the configuration)"},
		"the flag":          {flag: "process", where: "(drop --engine or UAH_ENGINE)"},
		"both, as embedded": {config: "engine = \"embedded\"\n", flag: "embedded", where: "(drop --engine or UAH_ENGINE and engine from the configuration)"},
		"an unknown engine": {flag: "turbo", where: "(drop --engine or UAH_ENGINE)"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, in := setupEnv(t)
			writeConfig(t, &in, c.config)
			in.Engine = c.flag

			res, err := app.Setup(context.Background(), in, io.Discard)
			require.NoError(t, err)
			assert.Equal(t, "embedded", res.Engine.Name())
			checks := app.Doctor(context.Background(), in, doctorOptions(t, filepath.Join(t.TempDir(), "trust.json")))
			i := slices.IndexFunc(checks, func(c app.Check) bool { return c.Name == "engine" })
			if c.where == "" {
				assert.Empty(t, res.Options.Notices)
				assert.Equal(t, -1, i, "no engine check without an engine setting")

				return
			}
			assert.Equal(t, []string{removed + " " + c.where}, res.Options.Notices)
			require.GreaterOrEqual(t, i, 0)
			assert.Equal(t, app.CheckWarn, checks[i].Status)
			assert.Equal(t, removed, checks[i].Detail)
		})
	}
}
