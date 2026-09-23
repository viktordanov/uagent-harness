package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/tui/bubble"
)

// tuiAction is the default action: open the terminal UI, optionally with a
// first prompt.
func tuiAction(ctx context.Context, cmd *cli.Command) error {
	if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		return cli.Exit("the TUI needs a terminal; for scripts and pipes use uah run", exitUsage)
	}
	st, err := resolveSetup(cmd, os.Stderr) // validates flags before the screen takes over
	if err != nil {
		return err
	}
	logFile, err := openTUILog(st.stateDir)
	if err != nil {
		return err
	}
	defer logFile.Close()

	deps := bubble.Deps{
		SessionID: st.options.ID,
		Prompt:    strings.TrimSpace(strings.Join(cmd.Args().Slice(), " ")),
		Open: func(ctx context.Context, id string) (*session.Session, []session.LoadedRun, error) {
			setup, err := setupFor(cmd, logFile, id)
			if err != nil {
				return nil, nil, err
			}
			s, err := session.Open(context.WithoutCancel(ctx), setup.engine, setup.options)
			if err != nil {
				return nil, nil, err
			}
			if !setup.options.Resumed {
				return s, nil, nil
			}
			history, err := session.Load(setup.stateDir, s.ID())
			if err != nil {
				_ = s.Close()

				return nil, nil, err
			}

			return s, history, nil
		},
		Sessions: func() ([]session.Info, error) { return session.Sessions(st.stateDir) },
	}
	if err := bubble.Run(ctx, deps); err != nil {
		return fmt.Errorf("the TUI stopped: %w", err)
	}

	return nil
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
