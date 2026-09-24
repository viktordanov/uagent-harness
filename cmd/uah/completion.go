package main

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/viktordanov/uagent/harness"

	"github.com/viktordanov/uagent-harness/internal/app"
	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/store"
)

// completeFlag is the shell-completion flag urfave/cli adds to every call
// the completion scripts make.
const completeFlag = "--generate-shell-completion"

// withCompletion sets every command's completer: flag values and session
// IDs, and otherwise urfave/cli's commands and flags.
func withCompletion(cmd *cli.Command) *cli.Command {
	cmd.ShellComplete = complete
	for _, sub := range cmd.Commands {
		withCompletion(sub)
	}

	return cmd
}

func complete(ctx context.Context, cmd *cli.Command) {
	if values := completionValues(ctx, cmd, previousWord(os.Args)); values != nil {
		for _, v := range values {
			fmt.Fprintln(cmd.Root().Writer, v)
		}

		return
	}
	cli.DefaultCompleteWithFlags(ctx, cmd)
}

// previousWord is the word before the one being completed: the argument just
// before the completion flag.
func previousWord(args []string) string {
	i := slices.Index(args, completeFlag)
	if i < 2 {
		return ""
	}

	return args[i-1]
}

// completionValues lists the values the word after prev can take, or nil
// when prev is not a flag with known values.
func completionValues(ctx context.Context, cmd *cli.Command, prev string) []string {
	switch strings.TrimLeft(prev, "-") {
	case "provider":
		return session.Providers
	case "effort", "e":
		return session.Efforts
	case "sandbox":
		return sandboxModes()
	case "engine":
		return app.Engines
	case "ask":
		return approval.Policies
	case "log-level":
		return slices.Sorted(maps.Keys(app.LogLevels))
	case "session", "s":
		return sessionIDs(ctx, cmd)
	}
	if cmd.Name == "show" {
		return sessionIDs(ctx, cmd)
	}

	return nil
}

// sessionIDs are the known sessions' short IDs, most recent first.
func sessionIDs(ctx context.Context, cmd *cli.Command) []string {
	// Completion mode skips flag sources, so read the variable directly.
	dir := cmd.String("state-dir")
	if v := os.Getenv("UAGENT_STATE_DIR"); v != "" && !cmd.IsSet("state-dir") {
		dir = v
	}
	if dir == "" {
		dir = harness.DefaultStateDir()
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return []string{}
	}
	infos, err := store.List(ctx, abs)
	if err != nil {
		return []string{}
	}
	ids := make([]string, 0, len(infos))
	for _, in := range infos {
		ids = append(ids, short(in.ID))
	}

	return ids
}
