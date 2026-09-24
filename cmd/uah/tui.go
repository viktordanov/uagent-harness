package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/viktordanov/uagent-harness/internal/models"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/store"
	"github.com/viktordanov/uagent-harness/internal/tui/bubble"
)

// tuiAction is the default action: open the terminal UI, optionally with a
// first prompt.
func tuiAction(ctx context.Context, cmd *cli.Command) error {
	return openTUI(ctx, cmd, tuiLaunch{
		sessionRef: cmd.String("session"),
		prompt:     strings.TrimSpace(strings.Join(cmd.Args().Slice(), " ")),
	})
}

// tuiLaunch says what the TUI shows first.
type tuiLaunch struct {
	sessionRef string // a session to resume, by ID or unique prefix
	prompt     string
	picker     bool // start in the session picker
	all        bool // the picker shows every directory
}

func openTUI(ctx context.Context, cmd *cli.Command, launch tuiLaunch) error {
	if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		return cli.Exit("the TUI needs a terminal; for scripts and pipes use uah run", exitUsage)
	}
	st, err := setupFor(ctx, cmd, os.Stderr, launch.sessionRef) // validates flags before the screen takes over
	if err != nil {
		return err
	}
	logFile, err := openTUILog(st.StateDir)
	if err != nil {
		return err
	}
	defer logFile.Close()
	cwd, err := currentDir(cmd)
	if err != nil {
		return err
	}

	deps := bubble.Deps{
		SessionID:   st.Options.ID,
		Prompt:      launch.prompt,
		Cwd:         cwd,
		Picker:      launch.picker,
		AllSessions: launch.all,
		Details:     st.Config.TUI.Details,
		Mouse:       st.Config.TUI.Mouse,
		Version:     buildVersion(),
		Config:      tuiConfig(cmd),
		SaveConfig:  tuiSaveConfig(ctx, cmd),
		Open: func(ctx context.Context, id string) (*session.Session, []session.LoadedRun, error) {
			setup, err := setupFor(ctx, cmd, logFile, id)
			if err != nil {
				return nil, nil, err
			}
			setup.Options.Source, setup.Options.Interactive = session.SourceTUI, true
			s, err := session.Open(context.WithoutCancel(ctx), setup.Engine, setup.Options)
			if err != nil {
				return nil, nil, err
			}
			if !setup.Options.Resumed {
				return s, nil, nil
			}
			history, err := session.Load(setup.StateDir, s.ID())
			if err != nil {
				_ = s.Close()

				return nil, nil, err
			}

			return s, history, nil
		},
		// The session's catalog caches for five minutes and bounds a
		// refresh to five seconds.
		Models: func(ctx context.Context, provider string) models.Catalog {
			return st.Models.Catalog(ctx, provider, models.OnlineIfUncached)
		},
		Windows:  st.Models.Window,
		Activity: func() (map[string]int, error) { return store.ActivityIn(ctx, st.StateDir, time.Now(), 7*12) },
		Sessions: func() ([]session.Info, error) {
			infos, err := store.List(ctx, st.StateDir)

			return session.Interactive(infos), err
		},
	}
	if err := bubble.Run(ctx, deps); err != nil {
		return fmt.Errorf("the TUI stopped: %w", err)
	}

	return nil
}

// currentDir is the directory sessions are scoped to: --workspace when given,
// otherwise the working directory.
func currentDir(cmd *cli.Command) (string, error) {
	dir := "."
	if w := cmd.String("workspace"); w != "" {
		dir = w
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve the current directory: %w", err)
	}

	return abs, nil
}

// openTUILog opens the diagnostic log; the screen belongs to the TUI.
func openTUILog(stateDir string) (*os.File, error) {
	dir := filepath.Join(stateDir, "logs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "uah-tui.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("failed to open log: %w", err)
	}

	return f, nil
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()

	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
