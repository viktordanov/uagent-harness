package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"
	"github.com/viktordanov/unreal-agent/harness/llm"

	"github.com/viktordanov/uah/internal/compaction/eval"
	"github.com/viktordanov/uah/internal/compaction/evalrun"
	"github.com/viktordanov/uah/internal/home"
)

// compactionCommand is the hidden `uah compaction eval`: the compaction
// strategies compared on recorded sessions, as tables of numbers.
func compactionCommand() *cli.Command {
	return &cli.Command{
		Name:   "compaction",
		Usage:  "evaluate compaction on recorded sessions",
		Hidden: true,
		Commands: []*cli.Command{{
			Name:      "eval",
			Usage:     "compare the compaction strategies on recorded sessions",
			ArgsUsage: "[session file or directory]",
			Description: "Cuts each session (default: every session in uah's home) at its recorded compactions and at the\n" +
				"requests that first reached 50k, 100k, 150k, and 200k input tokens, captures the request the\n" +
				"embedded engine builds there, and applies each strategy. Summaries come from the sessions' own\n" +
				"compaction logs, the cache, or a stub; --summary-model writes the missing ones with a real model and\n" +
				"caches them. Prints tables of numbers only, never session content.",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "cache", Usage: "the summary cache directory (default: <uah home>/cache/compaction-eval)"},
				&cli.Int64Flag{Name: "window", Usage: "the context window in tokens", Value: 272_000},
				&cli.StringFlag{Name: "summary-model", Usage: "write missing summaries with this model (real model calls)"},
				&cli.StringFlag{Name: "summary-provider", Usage: "the provider for --summary-model", Value: "openai-codex"},
				&cli.StringFlag{Name: "summary-effort", Usage: "the effort for --summary-model", Value: "medium"},
				&cli.StringFlag{Name: "prompt-file", Usage: "a summary prompt to evaluate instead of Codex's"},
				&cli.StringSliceFlag{Name: "strategy", Usage: "evaluate only this strategy (repeatable)"},
				&cli.Int64Flag{Name: "max-case-tokens", Usage: "skip cases whose request is larger (bounds --summary-model's cost)"},
			},
			OnUsageError: onUsageError,
			Action:       compactionEvalAction,
		}},
	}
}

func compactionEvalAction(ctx context.Context, cmd *cli.Command) error {
	target := cmd.Args().First()
	if target == "" {
		target = filepath.Join(home.Dir(), "sessions")
	}
	cache := cmd.String("cache")
	if cache == "" {
		cache = filepath.Join(home.Dir(), "cache", "compaction-eval")
	}
	sums := &evalrun.Summaries{Dir: cache}
	if f := cmd.String("prompt-file"); f != "" {
		data, err := os.ReadFile(f)
		if err != nil {
			return cli.Exit(fmt.Sprintf("failed to read the prompt: %v", err), exitUsage)
		}
		sums.Prompt = strings.TrimSpace(string(data))
	}
	if model := cmd.String("summary-model"); model != "" {
		live, closeLive, err := evalrun.Live(cmd.String("summary-provider"), model, llm.ReasoningEffort(cmd.String("summary-effort")), sums.Prompt, os.Getenv)
		if err != nil {
			return err
		}
		defer func() { _ = closeLive() }()
		sums.Live = live
	}
	rep, err := evalrun.Run(ctx, target, evalrun.Options{Summaries: sums, Strategies: evalrun.Only(evalrun.Strategies(), cmd.StringSlice("strategy")), Window: cmd.Int64("window"), Progress: os.Stderr, MaxCaseTokens: cmd.Int64("max-case-tokens")})
	if err != nil {
		return err
	}
	w := os.Stdout
	fmt.Fprintf(w, "sessions: %d, cases: %d (skipped %d), recorded compactions reproduced: %d of %d\n",
		rep.Sessions, rep.Cases, rep.Skipped, rep.Reproduced, rep.Recorded)
	fmt.Fprintf(w, "summaries: %d recorded, %d cached, %d from the model, %d stubs\n\n",
		rep.Sources[evalrun.SourceRecorded], rep.Sources[evalrun.SourceCache], rep.Sources[evalrun.SourceModel], rep.Sources[evalrun.SourceStub])
	eval.Write(w, rep.Rows)

	return nil
}
