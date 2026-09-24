package main

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"

	"github.com/urfave/cli/v3"

	"github.com/viktordanov/uagent-harness/internal/app"
	"github.com/viktordanov/uagent-harness/internal/config"
	"github.com/viktordanov/uagent-harness/internal/mcp"
)

// mcpApproveCommand is `uah mcp approve <name> [tool] [--mode <mode>]`: it
// sets a server's default_tools_approval_mode, or one tool's approval_mode,
// in the file that configures the server, or prints them without --mode.
func mcpApproveCommand(common func(...cli.Flag) []cli.Flag) *cli.Command {
	return &cli.Command{
		Name:      "approve",
		Usage:     "show or set when a server's tools ask for approval",
		ArgsUsage: nameArg + " [tool]",
		Description: "With --mode, sets the server's default_tools_approval_mode, or the tool's approval_mode\n" +
			"when a tool is named, in the file that configures the server, keeping the rest of it as it\n" +
			"was. Without --mode, prints the current modes. The modes are Codex's: approve never asks,\n" +
			"prompt always asks, writes asks unless the tool is read-only, and auto (the default) asks\n" +
			"unless the tool's annotations say it is safe.",
		Flags:        common(&cli.StringFlag{Name: "mode", Usage: "approve, prompt, writes, or auto"}),
		OnUsageError: onUsageError, Action: mcpApprove,
	}
}

func mcpApprove(_ context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	if len(args) < 1 || len(args) > 2 {
		return cli.Exit("expected a server name and at most one tool (see --help)", exitUsage)
	}
	name, tool := args[0], ""
	if len(args) == 2 {
		tool = args[1]
	}
	ws, err := mcpWorkspace(cmd)
	if err != nil {
		return err
	}
	userFile := cmd.String(flagConfig)
	if !cmd.IsSet("mode") {
		cfg, _, err := config.Load(userFile, ws)
		if err != nil {
			return cli.Exit(err.Error(), exitUsage)
		}
		server, ok := cfg.MCPServers[name]
		if !ok {
			return cli.Exit(fmt.Sprintf("No MCP server named '%s' found.", name), exitUsage)
		}
		printApprovals(os.Stdout, name, tool, server)

		return nil
	}
	mode, err := mcp.ParseApprovalMode(cmd.String("mode"))
	if err != nil {
		return cli.Exit(err.Error(), exitUsage)
	}
	path, err := app.MCPServerFile(userFile, ws, name)
	if err != nil {
		return exitError(err)
	}
	if err := mcp.SetApproval(path, name, tool, mode); err != nil {
		return cli.Exit(err.Error(), exitUsage)
	}
	what := "default_tools_approval_mode"
	if tool != "" {
		what = "tools." + tool + ".approval_mode"
	}
	fmt.Printf("Set %s = %q for MCP server '%s' in %s.\n", what, mode, name, path)

	return nil
}

// printApprovals prints the server's default and per-tool modes, or one
// tool's mode and where it comes from.
func printApprovals(w io.Writer, name, tool string, c mcp.ServerConfig) {
	def := string(c.DefaultToolsApprovalMode)
	if def == "" {
		def = string(mcp.ApprovalAuto) + " (the default)"
	}
	if tool != "" {
		from := "the server's default"
		if c.Tools[tool].ApprovalMode != "" {
			from = "tools." + tool + ".approval_mode"
		}
		_, _ = fmt.Fprintf(w, "%s %s: %s (%s)\n", name, tool, c.ApprovalFor(tool), from)

		return
	}
	_, _ = fmt.Fprintf(w, "%s\n  default_tools_approval_mode: %s\n", name, def)
	for _, t := range slices.Sorted(maps.Keys(c.Tools)) {
		if m := c.Tools[t].ApprovalMode; m != "" {
			_, _ = fmt.Fprintf(w, "  tools.%s.approval_mode: %s\n", t, m)
		}
	}
}
