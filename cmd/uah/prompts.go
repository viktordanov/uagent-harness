package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/viktordanov/uagent-harness/internal/compaction"
	"github.com/viktordanov/uagent-harness/internal/config"
	"github.com/viktordanov/uagent-harness/internal/home"
	"github.com/viktordanov/uagent-harness/internal/instructions"
	"github.com/viktordanov/uagent-harness/internal/review"
)

// The `uah prompts` command's name, and the name of the subcommands that
// print one thing: `uah prompts show` and `uah sessions show`.
const (
	promptsName = "prompts"
	subShow     = "show"
)

// builtinPrompt is a prompt uah ships and the key that replaces it with a
// file.
type builtinPrompt struct {
	name  string // also the file's base name
	text  func() string
	table string // the key's table, "" for the top level
	key   string
	// alternative, when set, is a prompt uah does not use by default: its
	// key line is printed commented out, after the previous prompt's,
	// under this comment.
	alternative string
}

// builtinPrompts are in the order their keys can be written: top-level
// keys before any table.
var builtinPrompts = []builtinPrompt{
	{name: "compact", text: func() string { return compaction.Prompt + "\n" }, key: "experimental_compact_prompt_file"},
	{name: "system", text: func() string { return instructions.RunnerHostPrompt }, key: "model_instructions_file"},
	{
		name: "system-codex", text: func() string { return instructions.CodexPrompt }, key: "model_instructions_file",
		alternative: "Or Codex's own prompt (gpt-6-astra's; it names Codex's tools, see docs/configuration.md):",
	},
	{name: "review", text: review.DefaultPolicy, table: "review", key: "policy_file"},
}

// promptsCommand is `uah prompts`: it writes the built-in prompts into the
// configuration folder as a starting point, or prints one.
func promptsCommand() *cli.Command {
	return &cli.Command{
		Name:  promptsName,
		Usage: "write the built-in prompts into the config folder to customize them, or print one",
		Commands: []*cli.Command{
			{
				Name: "init", Usage: "write compact.md, system.md, system-codex.md, and review.md into <config dir>/prompts and print the keys that use them",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: flagConfig, Usage: usageConfig + "; the prompts go into its folder", Value: config.UserFile(), Sources: cli.EnvVars(home.EnvConfig), TakesFile: true},
					&cli.BoolFlag{Name: "force", Usage: "overwrite prompt files that exist"},
				},
				OnUsageError: onUsageError, Action: promptsInit,
			},
			{Name: subShow, Usage: "print a built-in prompt", ArgsUsage: strings.Join(promptNames(), "|"), OnUsageError: onUsageError, Action: promptsShow},
		},
	}
}

// promptNames are the built-in prompts' names.
func promptNames() []string {
	names := make([]string, 0, len(builtinPrompts))
	for _, p := range builtinPrompts {
		names = append(names, p.name)
	}

	return names
}

func promptsShow(_ context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	for _, p := range builtinPrompts {
		if cmd.Args().Len() == 1 && p.name == name {
			fmt.Print(p.text())

			return nil
		}
	}

	return cli.Exit("expected one of: "+strings.Join(promptNames(), ", "), exitUsage)
}

func promptsInit(_ context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() > 0 {
		return cli.Exit("prompts init takes no arguments (see --help)", exitUsage)
	}
	configFile := cmd.String(flagConfig)
	dir := filepath.Join(filepath.Dir(configFile), "prompts")
	force := cmd.Bool("force")
	if !force {
		for _, p := range builtinPrompts {
			path := filepath.Join(dir, p.name+".md")
			if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
				return cli.Exit(path+" exists; use --force to overwrite it", exitUsage)
			}
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create %s: %w", dir, err)
	}
	var lines []string
	for _, p := range builtinPrompts {
		path := filepath.Join(dir, p.name+".md")
		if err := os.WriteFile(path, []byte(p.text()), 0o600); err != nil {
			return fmt.Errorf("failed to write %s: %w", path, err)
		}
		fmt.Printf("Wrote %s\n", path)
		line := fmt.Sprintf("%s = %q", p.key, homePath(path))
		switch {
		case p.alternative != "":
			lines[len(lines)-1] += "\n# " + p.alternative + "\n# " + line
		case p.table != "":
			lines = append(lines, "["+p.table+"]\n"+line)
		default:
			lines = append(lines, line)
		}
	}
	fmt.Printf("\nTo use them, add to %s:\n\n%s\n", configFile, strings.Join(lines, "\n\n"))

	return nil
}

// homePath writes a path under the home directory as ~/..., which the
// prompt keys accept.
func homePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~/" + filepath.ToSlash(rest)
	}

	return path
}
