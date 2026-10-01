// Command perf is uah's performance harness: go run ./tools/perf. It
// builds sessions of known sizes, drives them through uah's real stack
// against a scripted fake model, and prints what loading, turns, the TUI,
// subagents, and idling cost, with a JSON report and an optional
// comparison against a baseline. See tools/perf/README.md.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/viktordanov/uah/tools/perf/perf"
)

// exitRegression is the exit status when a comparison finds regressions.
const exitRegression = 3

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "perf:", err)
		if errors.Is(err, errRegressed) {
			os.Exit(exitRegression)
		}
		os.Exit(1)
	}
}

var errRegressed = errors.New("regressions beyond the threshold")

func run() error {
	fs := flag.NewFlagSet("perf", flag.ContinueOnError)
	sizes := fs.String("sizes", "small,medium,large", "fixture sizes: small (100 records), medium (2,000), large (10,000)")
	match := fs.String("run", "", "run only the scenarios whose name matches this regular expression (e.g. 'load|tui')")
	count := fs.Int("count", 1, "run each scenario this many times and report the medians")
	out := fs.String("out", "", "write the JSON report here (default: tools/perf/results/<time>-<commit>.json)")
	baseline := fs.String("baseline", "", "compare with this saved report and exit 3 on a regression")
	threshold := fs.Float64("threshold", 0.25, "the relative change that counts as a regression or an improvement")
	all := fs.Bool("all", false, "with -baseline or -compare, list every metric, not only the changes")
	cpuprofile := fs.Bool("cpuprofile", false, "write a CPU profile of each scenario to the profile directory")
	memprofile := fs.Bool("memprofile", false, "write allocation profiles of each scenario (before and after; use go tool pprof -base)")
	goroutines := fs.Bool("goroutines", false, "write the stacks of the goroutines each scenario leaves behind to the profile directory")
	profileDir := fs.String("profiles", "", "the profile directory (default: next to the report)")
	realHome := fs.String("real", "", "also load copies of the largest sessions of this uah home (read-only; e.g. ~/.uah)")
	realN := fs.Int("real-sessions", 3, "with -real, how many sessions")
	keep := fs.Bool("keep", false, "keep the scratch directory")
	compare := fs.Bool("compare", false, "compare two saved reports (arguments: baseline new) without running")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: go run ./tools/perf [flags]\n       go run ./tools/perf -compare baseline.json new.json\n\nflags:")
		fs.PrintDefaults()
	}
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	if *compare {
		return compareFiles(fs.Args(), *threshold, *all)
	}
	opts := perf.Options{Count: *count, CPUProfile: *cpuprofile, MemProfile: *memprofile, Goroutines: *goroutines, Keep: *keep, Real: *realHome, RealSessions: *realN, Log: os.Stderr}
	for name := range strings.SplitSeq(*sizes, ",") {
		size, ok := sizeNamed(strings.TrimSpace(name))
		if !ok {
			return fmt.Errorf("unknown size %q (want small, medium, or large)", name)
		}
		opts.Sizes = append(opts.Sizes, size)
	}
	if *match != "" {
		re, err := regexp.Compile(*match)
		if err != nil {
			return fmt.Errorf("bad -run: %w", err)
		}
		opts.Match = re
	}
	var old *perf.Report
	if *baseline != "" {
		var err error
		if old, err = perf.LoadReport(*baseline); err != nil {
			return err
		}
	}
	path := *out
	if path == "" {
		path = filepath.Join(resultsDir(), time.Now().Format("20060102-150405")+".json")
	}
	opts.ProfileDir = *profileDir
	if opts.ProfileDir == "" {
		opts.ProfileDir = strings.TrimSuffix(path, ".json") + "-profiles"
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	rep, err := perf.Run(ctx, opts)
	if rep != nil && len(rep.Results) > 0 {
		rep.Write(os.Stdout)
		if serr := os.MkdirAll(filepath.Dir(path), 0o700); serr == nil {
			serr = rep.Save(path)
			if serr == nil {
				fmt.Fprintln(os.Stderr, "report:", path)
			}
			err = errors.Join(err, serr)
		}
	}
	if err != nil || old == nil {
		return err
	}
	fmt.Println()
	changes := perf.Compare(old, rep, *threshold)
	perf.WriteChanges(os.Stdout, changes, *all)
	if perf.Regressions(changes) > 0 {
		return errRegressed
	}

	return nil
}

func compareFiles(args []string, threshold float64, all bool) error {
	if len(args) != 2 {
		return errors.New("-compare takes two reports: baseline new")
	}
	old, err := perf.LoadReport(args[0])
	if err != nil {
		return err
	}
	cur, err := perf.LoadReport(args[1])
	if err != nil {
		return err
	}
	changes := perf.Compare(old, cur, threshold)
	perf.WriteChanges(os.Stdout, changes, all)
	if perf.Regressions(changes) > 0 {
		return errRegressed
	}

	return nil
}

func sizeNamed(name string) (perf.Size, bool) {
	for _, s := range perf.Sizes {
		if s.Name == name {
			return s, true
		}
	}

	return perf.Size{}, false
}

// resultsDir is tools/perf/results under the module root (the working
// directory when the module root cannot be found).
func resultsDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return "results"
	}
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return filepath.Join(d, "tools", "perf", "results")
		}
		if filepath.Dir(d) == d {
			return filepath.Join(dir, "results")
		}
	}
}
