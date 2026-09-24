package main

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/viktordanov/uagent/harness"

	"github.com/viktordanov/uagent-harness/internal/app"
	"github.com/viktordanov/uagent-harness/internal/config"
	"github.com/viktordanov/uagent-harness/internal/session"
)

// sessionFlags are shared by the TUI and `uah run`.
func sessionFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name: "provider", Usage: "LLM provider: " + strings.Join(session.Providers, ", "),
			DefaultText: app.CodexProvider + ", or the resumed session's", Sources: cli.EnvVars("UNREAL_HARNESS_LLM_PROVIDER"),
			Validator: oneOf("provider", session.Providers),
		},
		&cli.StringFlag{
			Name: "model", Aliases: []string{"m"}, Usage: "model ID",
			DefaultText: app.DefaultCodexModel + " for openai-codex, or the resumed session's", Sources: cli.EnvVars("UNREAL_HARNESS_LLM_MODEL"),
		},
		&cli.StringFlag{
			Name: "effort", Aliases: []string{"e"}, Usage: "thinking level: " + strings.Join(session.Efforts, ", "),
			DefaultText: app.DefaultEffort + ", or the resumed session's", Validator: oneOf("effort", session.Efforts),
		},
		&cli.StringFlag{
			Name: flagWorkspace, Aliases: []string{"C"}, Usage: "agent workspace and Bash working directory",
			DefaultText: "the current directory, or the resumed session's", TakesFile: true,
		},
		&cli.StringFlag{Name: "session", Aliases: []string{"s"}, Usage: "resume a session by ID or unique ID prefix"},
		&cli.DurationFlag{Name: "timeout", Aliases: []string{"t"}, Value: 30 * time.Minute, Usage: "wall-clock limit per run (0 disables)"},
		&cli.StringFlag{
			Name: "state-dir", Usage: "sessions, logs, and run records; must be outside the workspace",
			Value: harness.DefaultStateDir(), Sources: cli.EnvVars("UAGENT_STATE_DIR"), TakesFile: true,
		},
		&cli.StringFlag{
			Name: "engine", Usage: "embedded (the runner's packages in process: live steering, effort, model, and /fast) or process (spawn unreal-agent-runner)",
			DefaultText: app.EngineEmbedded, Sources: cli.EnvVars("UAH_ENGINE"), Validator: oneOf("engine", app.Engines),
		},
		&cli.BoolFlag{Name: "fast", Usage: "priority processing (service_tier priority; embedded engine, openai and openai-codex)"},
		&cli.StringFlag{
			Name: "runner", Usage: "path to unreal-agent-runner, for the process engine", DefaultText: "~/.local/bin, then PATH",
			Sources: cli.EnvVars("UAGENT_RUNNER"), TakesFile: true,
		},
		&cli.StringFlag{
			Name: "base-url", Usage: "LLM base URL override", DefaultText: "provider default",
			Sources: cli.EnvVars("UNREAL_HARNESS_LLM_BASE_URL"),
		},
		&cli.StringFlag{
			Name: "max-disk", Usage: "stop a run when tool output exceeds this size, e.g. 500M (0 disables)", Value: "5G",
			Validator: func(v string) error {
				_, err := app.ParseSize(v)

				return err
			},
		},
		&cli.BoolFlag{Name: "allow-dotenv", Usage: "run even if the workspace .env sets risky variables"},
		&cli.StringFlag{
			Name: "config", Usage: "user configuration file", Value: config.UserFile(),
			Sources: cli.EnvVars("UAGENT_CONFIG"), TakesFile: true,
		},
		&cli.BoolFlag{Name: "no-instructions", Usage: "do not load AGENTS.md or CLAUDE.md files"},
		&cli.StringFlag{Name: "log-level", Usage: "diagnostic log level: debug, info, warn, error", Value: "warn", Validator: oneOfMap("log-level", app.LogLevels)},
	}
}

// setupFor sets up a session for ref ("" starts a new one) from the flags,
// mapping usage errors to exitUsage.
func setupFor(cmd *cli.Command, logOutput io.Writer, ref string) (app.Result, error) {
	in := inputs(cmd)
	in.SessionRef = ref
	st, err := app.Setup(in, logOutput)
	if err != nil {
		return app.Result{}, exitError(err)
	}

	return st, nil
}

// exitError maps an app.UsageError to exitUsage with the same message.
func exitError(err error) error {
	if _, ok := errors.AsType[*app.UsageError](err); ok {
		return cli.Exit(err.Error(), exitUsage)
	}

	return err
}

// inputs collects the session flags for app.Setup.
func inputs(cmd *cli.Command) app.Inputs {
	return app.Inputs{
		ConfigPath:     cmd.String("config"),
		StateDir:       cmd.String("state-dir"),
		SessionRef:     cmd.String("session"),
		LogLevel:       cmd.String("log-level"),
		Provider:       cmd.String("provider"),
		Model:          cmd.String("model"),
		Effort:         cmd.String("effort"),
		Workspace:      cmd.String(flagWorkspace),
		Engine:         cmd.String("engine"),
		Runner:         cmd.String("runner"),
		BaseURL:        cmd.String("base-url"),
		Timeout:        cmd.Duration("timeout"),
		TimeoutSet:     cmd.IsSet("timeout"),
		MaxDisk:        cmd.String("max-disk"),
		MaxDiskSet:     cmd.IsSet("max-disk"),
		Fast:           cmd.Bool("fast"),
		FastSet:        cmd.IsSet("fast"),
		AllowDotenv:    cmd.Bool("allow-dotenv"),
		NoInstructions: cmd.Bool("no-instructions"),
	}
}

// oneOf accepts one of allowed, or empty (unset).
func oneOf(flag string, allowed []string) func(string) error {
	return func(v string) error {
		if v != "" && !slices.Contains(allowed, v) {
			return fmt.Errorf("invalid --%s %q (want %s)", flag, v, strings.Join(allowed, ", "))
		}

		return nil
	}
}

func oneOfMap[V any](flag string, allowed map[string]V) func(string) error {
	return func(v string) error {
		if _, ok := allowed[v]; !ok {
			return fmt.Errorf("invalid --%s %q", flag, v)
		}

		return nil
	}
}

func defaultStateDir() string { return harness.DefaultStateDir() }
