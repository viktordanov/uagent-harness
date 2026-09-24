package main

import (
	"context"

	"github.com/urfave/cli/v3"

	"github.com/viktordanov/uagent-harness/internal/app"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// tuiConfig loads what /config shows: the values a session opened now
// would use (the flags, then the files), with their sources, as `uah
// config` explains them.
func tuiConfig(cmd *cli.Command) func(context.Context) state.ConfigLoaded {
	return func(ctx context.Context) state.ConfigLoaded {
		in := inputs(cmd)
		in.SessionRef = ""
		rep, err := app.Inspect(ctx, in)
		if err != nil {
			return state.ConfigLoaded{Err: err}
		}
		values := make(map[string]state.ConfigValue, len(rep.Settings))
		for _, s := range rep.Settings {
			values[s.Key] = state.ConfigValue{Value: s.Text(), Source: s.SourceText()}
		}

		return state.ConfigLoaded{Path: rep.UserFile.Path, Values: values}
	}
}

// tuiSaveConfig writes one key to the user file with the editor `uah mcp
// add` uses, keeping its comments.
func tuiSaveConfig(ctx context.Context, cmd *cli.Command) func(key string, value any) error {
	return func(key string, value any) error {
		return app.SaveSetting(ctx, inputs(cmd), key, value) //nolint:wrapcheck // SaveSetting says what failed
	}
}
