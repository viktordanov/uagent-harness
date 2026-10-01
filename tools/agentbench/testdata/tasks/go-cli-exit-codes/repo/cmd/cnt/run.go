package main

import (
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
		fmt.Fprintln(stdout, "cnt:", err)
		fmt.Fprintln(stdout, usage)

		return 1
	}
	if fs.NArg() == 0 {
		fmt.Fprintln(stdout, usage)

		return 1
	}
	res := count.Files(fs.Args())
	status := 0
	total := 0
	for _, r := range res {
		if r.Err != nil {
			fmt.Fprintln(stdout, "cnt:", r.Err)

			continue
		}
		fmt.Fprintf(stdout, "%d %s\n", r.Lines, r.Name)
		total += r.Lines
		if *max >= 0 && r.Lines > *max {
			fmt.Fprintf(stderr, "cnt: %s: %d lines exceeds -max %d\n", r.Name, r.Lines, *max)
			status = 1
		}
	}
	if len(res) > 1 {
		fmt.Fprintf(stdout, "%d total\n", total)
	}

	return status
}
