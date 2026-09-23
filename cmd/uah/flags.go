package main

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/viktordanov/uagent/harness"

	"github.com/viktordanov/uagent-harness/internal/config"
	"github.com/viktordanov/uagent-harness/internal/engine/process"
	"github.com/viktordanov/uagent-harness/internal/instructions"
	"github.com/viktordanov/uagent-harness/internal/session"
)

const (
	codexProvider     = "openai-codex"
	defaultCodexModel = "gpt-6-sol"
	defaultEffort     = "high"
)

var logLevels = map[string]slog.Level{"debug": slog.LevelDebug, "info": slog.LevelInfo, "warn": slog.LevelWarn, "error": slog.LevelError}

// sessionFlags are shared by the TUI and `uah run`.
func sessionFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name: "provider", Usage: "LLM provider: " + strings.Join(session.Providers, ", "),
			DefaultText: codexProvider + ", or the resumed session's", Sources: cli.EnvVars("UNREAL_HARNESS_LLM_PROVIDER"),
			Validator: oneOf("provider", session.Providers),
		},
		&cli.StringFlag{
			Name: "model", Aliases: []string{"m"}, Usage: "model ID",
			DefaultText: defaultCodexModel + " for openai-codex, or the resumed session's", Sources: cli.EnvVars("UNREAL_HARNESS_LLM_MODEL"),
		},
		&cli.StringFlag{
			Name: "effort", Aliases: []string{"e"}, Usage: "thinking level: " + strings.Join(session.Efforts, ", "),
			DefaultText: defaultEffort + ", or the resumed session's", Validator: oneOf("effort", session.Efforts),
		},
		&cli.StringFlag{
			Name: "workspace", Aliases: []string{"C"}, Usage: "agent workspace and Bash working directory",
			DefaultText: "the current directory, or the resumed session's", TakesFile: true,
		},
		&cli.StringFlag{Name: "session", Aliases: []string{"s"}, Usage: "resume a session by ID or unique ID prefix"},
		&cli.DurationFlag{Name: "timeout", Aliases: []string{"t"}, Value: 30 * time.Minute, Usage: "wall-clock limit per run (0 disables)"},
		&cli.StringFlag{
			Name: "state-dir", Usage: "sessions, logs, and run records; must be outside the workspace",
			Value: harness.DefaultStateDir(), Sources: cli.EnvVars("UAGENT_STATE_DIR"), TakesFile: true,
		},
		&cli.StringFlag{
			Name: "runner", Usage: "path to unreal-agent-runner", DefaultText: "~/.local/bin, then PATH",
			Sources: cli.EnvVars("UAGENT_RUNNER"), TakesFile: true,
		},
		&cli.StringFlag{
			Name: "base-url", Usage: "LLM base URL override", DefaultText: "provider default",
			Sources: cli.EnvVars("UNREAL_HARNESS_LLM_BASE_URL"),
		},
		&cli.StringFlag{
			Name: "max-disk", Usage: "stop a run when tool output exceeds this size, e.g. 500M (0 disables)", Value: "5G",
			Validator: func(v string) error {
				_, err := parseSize(v)

				return err
			},
		},
		&cli.BoolFlag{Name: "allow-dotenv", Usage: "run even if the workspace .env sets risky variables"},
		&cli.StringFlag{
			Name: "config", Usage: "user configuration file", Value: config.UserFile(),
			Sources: cli.EnvVars("UAGENT_CONFIG"), TakesFile: true,
		},
		&cli.BoolFlag{Name: "no-instructions", Usage: "do not load AGENTS.md or CLAUDE.md files"},
		&cli.StringFlag{Name: "log-level", Usage: "diagnostic log level: debug, info, warn, error", Value: "warn", Validator: oneOfMap("log-level", logLevels)},
	}
}

// setup is everything needed to open a session.
type setup struct {
	stateDir string
	engine   *process.Engine
	options  session.Options
}

// resolveSetup combines flags, the environment, the resumed session (from
// --session), the configuration file, and defaults, in that order.
func resolveSetup(cmd *cli.Command, logOutput io.Writer) (setup, error) {
	return setupFor(cmd, logOutput, cmd.String("session"))
}

// setupFor is resolveSetup for the session ref ("" starts a new session).
func setupFor(cmd *cli.Command, logOutput io.Writer, ref string) (setup, error) {
	stateDir, err := filepath.Abs(cmd.String("state-dir"))
	if err != nil {
		return setup{}, fmt.Errorf("failed to resolve state dir: %w", err)
	}
	var resumed session.Info
	opts := session.Options{}
	if ref != "" {
		info, err := resolveSession(stateDir, ref)
		if err != nil {
			return setup{}, err
		}
		resumed, opts.ID, opts.Resumed = info, info.ID, true
	}

	workspace, err := filepath.Abs(pick(cmd, "workspace", resumed.Workspace, "", "."))
	if err != nil {
		return setup{}, fmt.Errorf("failed to resolve workspace: %w", err)
	}
	cfg, _, err := config.Load(cmd.String("config"), workspace)
	if err != nil {
		return setup{}, cli.Exit(err.Error(), exitUsage)
	}

	settings := session.Settings{
		Provider:    pick(cmd, "provider", resumed.Provider, cfg.Provider, codexProvider),
		Effort:      pick(cmd, "effort", resumed.Effort, cfg.Effort, defaultEffort),
		Workspace:   workspace,
		BaseURL:     cmd.String("base-url"),
		Timeout:     cmd.Duration("timeout"),
		AllowDotenv: cmd.Bool("allow-dotenv"),
	}
	modelDefault := ""
	if settings.Provider == codexProvider {
		modelDefault = defaultCodexModel
	}
	if cmd.String("provider") == "" || settings.Provider == resumed.Provider {
		settings.Model = pick(cmd, "model", resumed.Model, cfg.Model, modelDefault)
	} else {
		settings.Model = pick(cmd, "model", "", "", modelDefault)
	}
	if !cmd.IsSet("timeout") {
		if d, ok, err := cfg.TimeoutValue(); err != nil {
			return setup{}, cli.Exit(err.Error(), exitUsage)
		} else if ok {
			settings.Timeout = d
		}
	}
	if !cmd.Bool("no-instructions") && cfg.InstructionsEnabled() {
		loaded, prompt, err := loadInstructions(workspace, cfg.Instructions.MaxBytes)
		if err != nil {
			return setup{}, err
		}
		settings.SystemPrompt = prompt
		opts.Instructions = loaded
	}
	if err := settings.Validate(); err != nil {
		return setup{}, cli.Exit(err.Error(), exitUsage)
	}
	opts.Settings = settings

	runner, err := harness.FindRunner(cmd.String("runner"))
	if err != nil {
		return setup{}, cli.Exit(err.Error(), exitUsage)
	}
	maxDiskText := cmd.String("max-disk")
	if !cmd.IsSet("max-disk") && cfg.MaxDisk != "" {
		maxDiskText = cfg.MaxDisk
	}
	maxDisk, err := parseSize(maxDiskText)
	if err != nil {
		return setup{}, cli.Exit("max_disk: "+err.Error(), exitUsage)
	}
	logger := slog.New(slog.NewTextHandler(logOutput, &slog.HandlerOptions{Level: logLevels[cmd.String("log-level")]}))
	eng := process.New(harness.Config{RunnerPath: runner, StateDir: stateDir, MaxDisk: maxDisk, Logger: logger})

	return setup{stateDir: stateDir, engine: eng, options: opts}, nil
}

// pick returns the first value that is set: the flag (by flag or
// environment; an empty variable counts as unset), the resumed session's
// value, the configuration file's value, then the default.
func pick(cmd *cli.Command, flag, resumed, configured, fallback string) string {
	if v := cmd.String(flag); cmd.IsSet(flag) && v != "" {
		return v
	}
	for _, v := range []string{resumed, configured} {
		if v != "" {
			return v
		}
	}

	return fallback
}

// loadInstructions discovers and assembles instruction files, returning the
// event to report and the host prompt ("" when there are none).
func loadInstructions(workspace string, maxBytes int) (*session.InstructionsLoaded, string, error) {
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		if home, err := os.UserHomeDir(); err == nil {
			codexHome = filepath.Join(home, ".codex")
		}
	}
	userFiles := []string{filepath.Join(config.Dir(), "AGENTS.md")}
	if codexHome != "" {
		userFiles = append(userFiles, filepath.Join(codexHome, "AGENTS.md"))
	}
	files, err := instructions.Discover(workspace, userFiles)
	if err != nil {
		return nil, "", fmt.Errorf("failed to find instructions: %w", err)
	}
	if len(files) == 0 {
		return nil, "", nil
	}
	text, used, truncated, err := instructions.Assemble(files, maxBytes)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read instructions: %w", err)
	}
	paths := make([]string, 0, len(used))
	for _, f := range used {
		paths = append(paths, f.Path)
	}

	return &session.InstructionsLoaded{Files: paths, Bytes: len(text), Truncated: truncated}, instructions.HostPrompt(text), nil
}

// resolveSession finds a session by exact ID or unique prefix.
func resolveSession(stateDir, ref string) (session.Info, error) {
	infos, err := session.Sessions(stateDir)
	if err != nil {
		return session.Info{}, err
	}
	var matches []session.Info
	for _, info := range infos {
		if info.ID == ref {
			return info, nil
		}
		if strings.HasPrefix(info.ID, ref) {
			matches = append(matches, info)
		}
	}
	switch len(matches) {
	case 0:
		return session.Info{}, cli.Exit(fmt.Sprintf("no session matches %q (see uah sessions)", ref), exitUsage)
	case 1:
		return matches[0], nil
	}

	return session.Info{}, cli.Exit(fmt.Sprintf("%q matches %d sessions; use more of the ID", ref, len(matches)), exitUsage)
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

// parseSize parses sizes like 500M, 5G, or 1024 (bytes). "0" disables the limit.
func parseSize(s string) (int64, error) {
	s = strings.ToUpper(strings.TrimSuffix(strings.TrimSpace(s), "B"))
	mult := int64(1)
	if n := len(s); n > 0 {
		switch s[n-1] {
		case 'K':
			mult, s = 1<<10, s[:n-1]
		case 'M':
			mult, s = 1<<20, s[:n-1]
		case 'G':
			mult, s = 1<<30, s[:n-1]
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0, errors.New("invalid size " + strconv.Quote(s))
	}

	return int64(v * float64(mult)), nil
}

func defaultStateDir() string { return harness.DefaultStateDir() }
