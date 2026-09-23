package main

import (
	"context"

	"github.com/urfave/cli/v3"
)

// tuiAction is the default action: open the terminal UI.
func tuiAction(context.Context, *cli.Command) error {
	return notImplemented("the TUI", "M4")
}
