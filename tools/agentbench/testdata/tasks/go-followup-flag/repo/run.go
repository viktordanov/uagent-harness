package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"example.com/logq/internal/entry"
	"example.com/logq/internal/filter"
	"example.com/logq/internal/format"
)

// Exit statuses.
const (
	exitOK    = 0
	exitRead  = 1
	exitUsage = 2
)

// run is logq: it parses args, reads the inputs, and prints the entries
// the filters keep. It returns the exit status.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("logq", flag.ContinueOnError)
	fs.SetOutput(stderr)
	level := fs.String("level", "", "keep entries at this level or above: debug, info, warn, error")
	grep := fs.String("grep", "", "keep entries whose message contains this text")
	formatName := fs.String("format", "text", "output format: text or json")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	var filters []filter.Filter
	if *level != "" {
		f, err := filter.MinLevel(*level)
		if err != nil {
			fmt.Fprintln(stderr, "logq:", err)

			return exitUsage
		}
		filters = append(filters, f)
	}
	if *grep != "" {
		filters = append(filters, filter.Contains(*grep))
	}
	printer, err := format.New(*formatName)
	if err != nil {
		fmt.Fprintln(stderr, "logq:", err)

		return exitUsage
	}
	entries, err := readAll(fs.Args(), stdin, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "logq:", err)

		return exitRead
	}
	for _, e := range filter.Apply(entries, filters...) {
		if err := printer.Print(stdout, e); err != nil {
			fmt.Fprintln(stderr, "logq:", err)

			return exitRead
		}
	}

	return exitOK
}

// readAll reads every input in order: the files, or stdin when there are
// none.
func readAll(files []string, stdin io.Reader, stderr io.Writer) ([]entry.Entry, error) {
	if len(files) == 0 {
		return entry.ReadAll(stdin, stderr)
	}
	var all []entry.Entry
	for _, name := range files {
		f, err := os.Open(name)
		if err != nil {
			return nil, err
		}
		es, err := entry.ReadAll(f, stderr)
		f.Close()
		if err != nil {
			return nil, errors.Join(fmt.Errorf("%s", name), err)
		}
		all = append(all, es...)
	}

	return all, nil
}
