package main

import (
	"context"

	"github.com/urfave/cli/v3"
)

// sessionsCommand is `uah sessions`: list saved sessions.
func sessionsCommand() *cli.Command {
	return &cli.Command{
		Name:         "sessions",
		Usage:        "list sessions",
		OnUsageError: onUsageError,
		Action: func(context.Context, *cli.Command) error {
			return notImplemented("sessions", "M2")
		},
	}
}
