package main

import (
	"context"

	"github.com/urfave/cli/v3"
)

// runCommand is `uah run`: a headless session that prints events like uagent.
func runCommand() *cli.Command {
	return &cli.Command{
		Name:         "run",
		Usage:        "run a headless session",
		OnUsageError: onUsageError,
		Action: func(context.Context, *cli.Command) error {
			return notImplemented("run", "M2")
		},
	}
}
