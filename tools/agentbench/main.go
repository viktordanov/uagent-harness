// Command agentbench is uah's agent-level benchmark: go run ./tools/agentbench.
// It runs a suite of small coding tasks with `uah exec` and `codex exec` on
// the same model and effort, checks each result, records each run's
// timeline (model requests, tool calls, overlap, tokens), and writes JSON
// results and a markdown report comparing the two. It makes real model
// calls unless -dry. See tools/agentbench/README.md.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/viktordanov/uah/tools/agentbench/bench"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "agentbench:", err)
		os.Exit(1)
	}
}

func run() error {
	fs := flag.NewFlagSet("agentbench", flag.ContinueOnError)
	tasksRe := fs.String("tasks", "", "run only the tasks whose name matches this regular expression")
	harness := fs.String("harness", "both", "uah, codex, or both")
	repeat := fs.Int("repeat", 1, "runs of each task per harness")
	model := fs.String("model", "gpt-6.1-sol", "model for both harnesses")
	effort := fs.String("effort", "high", "reasoning effort for both harnesses: low, medium, high, xhigh, max, ultra")
	mode := fs.String("mode", bench.ModeAuto, "permission mode of both: auto (a reviewer model decides what needs approval, as the owner runs uah) or workspace (the workspace-write sandbox, approvals refused)")
	parallel := fs.Int("parallel", 1, "runs at once (mind the rate limits)")
	timeout := fs.Duration("timeout", 15*time.Minute, "wall-clock limit of a run, unless the task sets one")
	maxRuns := fs.Int("max-runs", 60, "refuse to start more runs than this")
	dry := fs.Bool("dry", false, "validate the tasks without model calls: each check must fail on the untouched repository and pass on the reference solution")
	list := fs.Bool("list", false, "list the tasks and exit")
	out := fs.String("out", "", "results file, JSON lines; runs already in it are skipped (default: tools/agentbench/results/<model>-<effort>-<mode>.jsonl)")
	reportOnly := fs.Bool("report", false, "only write the markdown report of the results file")
	remeasure := fs.Bool("remeasure", false, "parse every run in the results file again (after a change to the parsers, the metrics, or the prices), then write the report")
	work := fs.String("work", filepath.Join(os.TempDir(), "uah-agentbench"), "scratch directory: workspaces, the shared Go build cache, uah's home")
	uahBin := fs.String("uah", "", "uah binary (default: built from this tree into the scratch directory)")
	codexBin := fs.String("codex", "codex", "codex binary")
	keep := fs.Bool("keep", false, "keep each run's workspace")
	var uahEnv envList
	fs.Var(&uahEnv, "uah-env", "KEY=VALUE added to uah's environment, such as UAH_ADAPTIVE_EFFORT=2-steps (repeatable; needs -variant)")
	var uahConfig envList
	fs.Var(&uahConfig, "uah-config", "a top-level line added to uah's generated config file, such as 'model_instructions_file = \"/path\"' (repeatable; needs -variant)")
	variant := fs.String("variant", "", "label of the uah runs, part of their results key, so they sit beside the control runs (no -variant) in one results file and the report compares them")
	priceIn := fs.Float64("price-in", 1.25, "USD per million uncached input tokens, for the cost estimate")
	priceCached := fs.Float64("price-cached", 0.125, "USD per million cached input tokens")
	priceOut := fs.Float64("price-out", 10, "USD per million output tokens (reasoning included)")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: go run ./tools/agentbench [flags]\n\nflags:")
		fs.PrintDefaults()
	}
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	root, err := repoRoot(context.Background())
	if err != nil {
		return err
	}
	if *out == "" {
		*out = filepath.Join(root, "tools", "agentbench", "results", *model+"-"+*effort+"-"+*mode+".jsonl")
	}
	price := bench.Price{Input: *priceIn, Cached: *priceCached, Output: *priceOut}
	if *remeasure {
		if err := bench.Remeasure(*out, price); err != nil {
			return err
		}
	}
	if *reportOnly || *remeasure {
		return writeReport(*out, price)
	}
	var re *regexp.Regexp
	if *tasksRe != "" {
		if re, err = regexp.Compile(*tasksRe); err != nil {
			return fmt.Errorf("bad -tasks: %w", err)
		}
	}
	tasks, err := bench.LoadTasks(filepath.Join(root, "tools", "agentbench", "testdata", "tasks"), re)
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		return errors.New("no task matches -tasks")
	}
	if *list {
		for _, t := range tasks {
			fmt.Printf("%-28s %-22s %s\n", t.Name, strings.Join(t.Tags, ","), t.Exercises)
		}

		return nil
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if *dry {
		vs, err := bench.Validate(ctx, tasks, *work, max(*parallel, 4))
		fmt.Print(bench.FormatValidation(vs))
		if err != nil {
			return err
		}
		for _, v := range vs {
			if !v.OK() {
				return errors.New("some tasks are not valid")
			}
		}

		return nil
	}
	cfg := bench.Config{
		Tasks: tasks, Repeat: *repeat, Model: *model, Effort: *effort, Parallel: *parallel, Timeout: *timeout,
		MaxRuns: *maxRuns, Work: *work, Out: *out, UAH: *uahBin, Codex: *codexBin, Price: price, Mode: *mode, Keep: *keep, Log: os.Stderr,
		UAHEnv: uahEnv, UAHConfig: uahConfig, Variant: *variant,
	}
	if err := harnesses(ctx, &cfg, *harness, root); err != nil {
		return err
	}
	runErr := bench.Execute(ctx, cfg)
	if err := writeReport(*out, price); err != nil {
		return errors.Join(runErr, err)
	}

	return runErr
}

func writeReport(out string, price bench.Price) error {
	results, err := bench.LoadResults(out)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		return fmt.Errorf("no results in %s", out)
	}
	md := strings.TrimSuffix(out, filepath.Ext(out)) + ".md"
	if err := os.WriteFile(md, []byte(bench.Report(results, price)), 0o644); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "report:", md)

	return nil
}

// harnesses checks the mode and the variant, sets the harnesses to run, and builds uah from this tree
// unless -uah names a binary.
func harnesses(ctx context.Context, cfg *bench.Config, harness, root string) error {
	if cfg.Mode != bench.ModeAuto && cfg.Mode != bench.ModeWorkspace {
		return fmt.Errorf("bad -mode %q (want auto or workspace)", cfg.Mode)
	}
	if len(cfg.UAHEnv)+len(cfg.UAHConfig) > 0 && cfg.Variant == "" {
		return errors.New("-uah-env and -uah-config need -variant, so their runs do not count as the control's")
	}
	switch harness {
	case "both":
		cfg.Harnesses = []string{bench.HarnessUAH, bench.HarnessCodex}
	case bench.HarnessUAH, bench.HarnessCodex:
		cfg.Harnesses = []string{harness}
	default:
		return fmt.Errorf("bad -harness %q (want uah, codex, or both)", harness)
	}
	if cfg.UAH != "" || !slices.Contains(cfg.Harnesses, bench.HarnessUAH) {
		return nil
	}
	cfg.UAH = filepath.Join(cfg.Work, "bin", "uah")
	fmt.Fprintln(os.Stderr, "building uah into", cfg.UAH)
	build := exec.CommandContext(ctx, "go", "build", "-o", cfg.UAH, "./cmd/uah")
	build.Dir, build.Stdout, build.Stderr = root, os.Stderr, os.Stderr
	if err := build.Run(); err != nil {
		return fmt.Errorf("build uah: %w", err)
	}

	return nil
}

// envList is a repeatable KEY=VALUE flag; a -uah-config line is one too
// (key = value).
type envList []string

func (l *envList) String() string { return strings.Join(*l, " ") }

func (l *envList) Set(v string) error {
	if name, _, ok := strings.Cut(v, "="); !ok || name == "" {
		return fmt.Errorf("want KEY=VALUE, got %q", v)
	}
	*l = append(*l, v)

	return nil
}

// repoRoot is the directory of the main module's go.mod.
func repoRoot(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "go", "env", "GOMOD").Output()
	if err != nil {
		return "", fmt.Errorf("go env GOMOD: %w", err)
	}
	mod := strings.TrimSpace(string(out))
	if mod == "" || mod == os.DevNull {
		return "", errors.New("run from inside the uah repository")
	}

	return filepath.Dir(mod), nil
}
