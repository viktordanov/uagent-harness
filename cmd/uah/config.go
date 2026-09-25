package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/urfave/cli/v3"

	"github.com/viktordanov/uagent-harness/internal/app"
)

// flagConfig names the user configuration file flag, and its command.
const flagConfig = "config"

// configCommand is `uah config`: the effective configuration and where each
// value came from.
func configCommand() *cli.Command {
	return &cli.Command{
		Name:  flagConfig,
		Usage: "show the effective configuration for a workspace and each value's source",
		Description: "Takes the same flags as a session and shows what a session started with them would use:\n" +
			"each key's value and its source (flag, env, session, project file, user file, or default).\n" +
			"Keys whose files add up list every file that set them. The reference is docs/configuration.md.",
		Flags:        append(sessionFlags(), &cli.BoolFlag{Name: flagJSON, Usage: "print JSON"}),
		OnUsageError: onUsageError,
		Action:       configAction,
	}
}

func configAction(ctx context.Context, cmd *cli.Command) error {
	rep, err := app.Inspect(ctx, inputs(cmd))
	if err != nil {
		return exitError(err)
	}
	if cmd.Bool(flagJSON) {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")

		return enc.Encode(rep)
	}

	return printReport(os.Stdout, rep)
}

// printReport writes the home, the workspace, and the files, then one line per key:
// key, value, and source.
func printReport(w io.Writer, rep app.Report) error {
	fmt.Fprintf(w, "home:         %s\n", rep.Home)
	fmt.Fprintf(w, "workspace:    %s (%s)\n", rep.Workspace, rep.WorkspaceSource)
	fmt.Fprintf(w, "user file:    %s (%s)\n", rep.UserFile.Path, rep.UserFile.State)
	fmt.Fprintf(w, "project file: %s (%s)\n\n", rep.ProjectFile.Path, rep.ProjectFile.State)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "KEY\tVALUE\tSOURCE")
	for _, s := range rep.Settings {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", s.Key, s.Text(), s.SourceText())
	}

	return tw.Flush()
}
