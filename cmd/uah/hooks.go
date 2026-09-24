package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/urfave/cli/v3"

	"github.com/viktordanov/uagent-harness/internal/app"
	"github.com/viktordanov/uagent-harness/internal/config"
	"github.com/viktordanov/uagent-harness/internal/hooks"
)

// hooksCommand is `uah hooks`: list the hooks for a workspace and trust project hooks.
func hooksCommand() *cli.Command {
	flags := []cli.Flag{
		&cli.StringFlag{Name: flagWorkspace, Aliases: []string{"C"}, Usage: "the workspace whose hooks to show", DefaultText: "the current directory", TakesFile: true},
		&cli.StringFlag{Name: "config", Usage: "user configuration file", Value: config.UserFile(), Sources: cli.EnvVars("UAGENT_CONFIG"), TakesFile: true},
	}

	return &cli.Command{
		Name:  "hooks",
		Usage: "list the hooks that apply to a workspace",
		Description: "Hooks come from [[hooks.<Event>]] entries in the user configuration and, for trusted\n" +
			"workspaces, in <workspace>/.uagent/config.toml. Project hooks run only after `uah hooks trust`\n" +
			"records their exact commands; a changed command needs trust again.",
		Flags:        flags,
		OnUsageError: onUsageError,
		Action:       listHooks,
		Commands: []*cli.Command{{
			Name:         "trust",
			Usage:        "allow this workspace's project hooks to run",
			Flags:        flags,
			OnUsageError: onUsageError,
			Action:       trustHooks,
		}},
	}
}

func loadHooks(cmd *cli.Command) (string, *hooks.Runner, *hooks.Trust, error) {
	dir := cmd.String(flagWorkspace)
	if dir == "" {
		dir = "."
	}
	workspace, err := filepath.Abs(dir)
	if err != nil {
		return "", nil, nil, fmt.Errorf("failed to resolve workspace: %w", err)
	}
	cfg, _, err := config.Load(cmd.String("config"), workspace)
	if err != nil {
		return "", nil, nil, cli.Exit(err.Error(), exitUsage)
	}
	list, err := cfg.HookList()
	if err != nil {
		return "", nil, nil, cli.Exit(err.Error(), exitUsage)
	}
	trust, err := hooks.LoadTrust(app.HookTrustFile())
	if err != nil {
		return "", nil, nil, err
	}
	runner, err := hooks.New(list, trust, workspace)
	if err != nil {
		return "", nil, nil, cli.Exit(err.Error(), exitUsage)
	}

	return workspace, runner, trust, nil
}

func listHooks(_ context.Context, cmd *cli.Command) error {
	workspace, runner, _, err := loadHooks(cmd)
	if err != nil {
		return err
	}
	list := runner.Hooks()
	if len(list) == 0 {
		fmt.Fprintln(os.Stderr, "no hooks for "+workspace)

		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "EVENT\tMATCHER\tSOURCE\tSTATE\tCOMMAND")
	for _, h := range list {
		state := "runs"
		if !runner.Trusted(h) {
			state = "untrusted"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", h.Event, h.Matcher, h.Source, state, oneLine(h.Command, 80))
	}

	return tw.Flush()
}

func trustHooks(_ context.Context, cmd *cli.Command) error {
	workspace, runner, trust, err := loadHooks(cmd)
	if err != nil {
		return err
	}
	var commands []string
	for _, h := range runner.Hooks() {
		if !runner.Trusted(h) {
			commands = append(commands, h.Command)
			fmt.Printf("trusting %s hook: %s\n", h.Event, h.Command)
		}
	}
	if len(commands) == 0 {
		fmt.Fprintln(os.Stderr, "no untrusted project hooks for "+workspace)

		return nil
	}

	return trust.Allow(workspace, commands...)
}
