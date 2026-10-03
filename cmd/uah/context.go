package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/urfave/cli/v3"

	"github.com/viktordanov/uah/internal/app"
	"github.com/viktordanov/uah/internal/contextprep"
)

// contextCommand is `uah context`: the modules of context preparation, and
// whether each applies to a new session in the workspace.
func contextCommand() *cli.Command {
	return &cli.Command{
		Name:  "context",
		Usage: "list context preparation's modules and whether each applies to a new session here",
		Description: "Lists every module of the prepared context a new session starts with: the built-ins\n" +
			"(replaced by a file of the same path under <config dir>/prompts/context), the library,\n" +
			"the user's <config dir>/prompts/context.d, and the project's .uah/context.d. For each it\n" +
			"says whether it is on, trusted, and applies to a new main session in the workspace, and\n" +
			"why. Module checks run in a read-only sandbox with no network. --show prints the context\n" +
			"a new main session and a read-only subagent would get.",
		Flags: append(sessionFlags(),
			&cli.BoolFlag{Name: flagJSON, Usage: "print the modules (and with --show, the context) as JSON"},
			&cli.BoolFlag{Name: subShow, Usage: "print the prepared context of a new main session and of a read-only subagent"},
		),
		OnUsageError: onUsageError,
		Action:       contextAction,
		Commands: []*cli.Command{{
			Name:         "trust",
			Usage:        "allow this workspace's project modules (.uah/context.d) as they are now",
			Flags:        sessionFlags(),
			OnUsageError: onUsageError,
			Action:       trustContext,
		}},
	}
}

func contextAction(ctx context.Context, cmd *cli.Command) error {
	p, err := app.PreviewContext(ctx, inputs(cmd), os.Getenv)
	if err != nil {
		return err
	}
	if !cmd.Bool(subShow) {
		p.Main, p.Subagent = "", ""
	}
	if cmd.Bool(flagJSON) {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(p); err != nil {
			return fmt.Errorf("failed to write the modules: %w", err)
		}

		return nil
	}
	if err := printModules(os.Stdout, p.Modules); err != nil {
		return err
	}
	if cmd.Bool(subShow) {
		fmt.Printf("\n── a new main session ──\n%s\n\n── a read-only subagent ──\n%s\n", orNone(p.Main), orNone(p.Subagent))
	}

	return nil
}

// printModules prints one line per module.
func printModules(w io.Writer, modules []contextprep.Status) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "BLOCK\tMODULE\tSOURCE\tSTATE\tAPPLIES\tWHY")
	for _, m := range modules {
		applies := "no"
		if m.Applies {
			applies = "yes"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", m.Block, m.Path, sourceLabel(m), stateLabel(m), applies, oneLine(m.Reason, 100))
	}

	return tw.Flush()
}

// sourceLabel names where a module comes from, and its file when it is not
// built in.
func sourceLabel(m contextprep.Status) string {
	label := string(m.Source)
	if m.Overrides {
		label += " (replaces the built-in)"
	}
	if m.File != "" {
		label += " " + homePath(m.File)
	}

	return label
}

// stateLabel is a module's STATE column.
func stateLabel(m contextprep.Status) string {
	switch {
	case strings.HasPrefix(m.Reason, "error: "):
		return "error"
	case !m.Trusted:
		return "untrusted"
	case !m.Enabled:
		return "off"
	}

	return "on"
}

func orNone(s string) string {
	if s == "" {
		return "(none: context preparation is off, or no block has anything to say)"
	}

	return s
}

func trustContext(_ context.Context, cmd *cli.Command) error {
	paths, err := app.TrustContextModules(inputs(cmd))
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		fmt.Fprintln(os.Stderr, "no untrusted project modules")

		return nil
	}
	for _, p := range paths {
		fmt.Printf("trusted %s\n", p)
	}

	return nil
}
