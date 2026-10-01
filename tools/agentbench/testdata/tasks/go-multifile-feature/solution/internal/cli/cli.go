// Package cli is the inventory command.
package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"

	"example.com/inventory/internal/format"
	"example.com/inventory/internal/store"
)

// Exit statuses.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

// Run runs the command with args (without the program name) and returns
// its exit status.
func Run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("inventory", flag.ContinueOnError)
	fs.SetOutput(stderr)
	sortBy := fs.String("sort", "name", "sort by name or qty")
	formatName := fs.String("format", "text", "output format: text or json")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: inventory [-sort name|qty] [-format text|json] FILE")

		return exitUsage
	}
	if *sortBy != "name" && *sortBy != "qty" {
		fmt.Fprintf(stderr, "unknown -sort %q\n", *sortBy)

		return exitUsage
	}
	write := format.Text
	switch *formatName {
	case "text":
	case "json":
		write = format.JSON
	default:
		fmt.Fprintf(stderr, "unknown -format %q\n", *formatName)

		return exitUsage
	}
	f, err := os.Open(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, err)

		return exitError
	}
	defer f.Close()
	items, err := store.Load(f)
	if err != nil {
		fmt.Fprintln(stderr, err)

		return exitError
	}
	sort.SliceStable(items, func(i, j int) bool {
		if *sortBy == "qty" {
			return items[i].Qty > items[j].Qty
		}

		return items[i].Name < items[j].Name
	})
	if err := write(stdout, items); err != nil {
		fmt.Fprintln(stderr, err)

		return exitError
	}

	return exitOK
}
