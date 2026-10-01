package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"example.com/cnt/internal/count"
)

const usage = "usage: cnt [-max N] FILE..."

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("cnt", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	max := fs.Int("max", -1, "maximum lines per file")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(stdout, usage)

			return 0
		}
		fmt.Fprintln(stderr, "cnt:", err)
		fmt.Fprintln(stderr, usage)

		return 2
	}
	limited := false
	fs.Visit(func(f *flag.Flag) { limited = limited || f.Name == "max" })
	if limited && *max < 0 {
		fmt.Fprintln(stderr, "cnt: -max must not be negative")
		fmt.Fprintln(stderr, usage)

		return 2
	}
	if fs.NArg() == 0 {
		fmt.Fprintln(stderr, "cnt: no files")
		fmt.Fprintln(stderr, usage)

		return 2
	}
	res := count.Files(fs.Args())
	failed, exceeded := false, false
	total := 0
	for _, r := range res {
		if r.Err != nil {
			fmt.Fprintln(stderr, "cnt:", r.Err)
			failed = true

			continue
		}
		fmt.Fprintf(stdout, "%d %s\n", r.Lines, r.Name)
		total += r.Lines
		if limited && r.Lines > *max {
			fmt.Fprintf(stderr, "cnt: %s: %d lines exceeds -max %d\n", r.Name, r.Lines, *max)
			exceeded = true
		}
	}
	if len(res) > 1 {
		fmt.Fprintf(stdout, "%d total\n", total)
	}
	switch {
	case failed:
		return 1
	case exceeded:
		return 3
	}

	return 0
}
