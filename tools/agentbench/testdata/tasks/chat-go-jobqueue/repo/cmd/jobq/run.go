package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"example.com/jobq/internal/clock"
	"example.com/jobq/queue"
	"example.com/jobq/store"
)

// Exit codes.
const (
	exitOK    = 0
	exitError = 1 // the command ran and failed
	exitUsage = 2 // the command line is wrong
)

// defaultStore is the store file when neither --store nor JOBQ_STORE is set.
const defaultStore = "jobq.json"

// newClock is the clock of every command; tests may replace it.
var newClock = func() clock.Clock { return clock.System() }

// command is one subcommand of jobq.
type command struct {
	name    string
	summary string
	run     func(args []string, stdout, stderr io.Writer) int
}

func commands() []command {
	return []command{
		{"add", "add a job: jobq add [flags] KIND [PAYLOAD]", cmdAdd},
		{"list", "list jobs, optionally of one state", cmdList},
		{"show", "print one job as JSON: jobq show ID", cmdShow},
		{"run", "run jobs until none is left to run", cmdRun},
		{"requeue", "put failed jobs back to pending: jobq requeue ID...", cmdRequeue},
		{"purge", "delete finished jobs of one state", cmdPurge},
	}
}

// run is jobq with its arguments (without the program name) and its output
// streams; it returns the exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return exitUsage
	}
	name, rest := args[0], args[1:]
	switch name {
	case "help", "-h", "-help", "--help":
		usage(stdout)
		return exitOK
	}
	for _, c := range commands() {
		if c.name == name {
			return c.run(rest, stdout, stderr)
		}
	}
	fmt.Fprintf(stderr, "jobq: unknown command %q\n", name)
	usage(stderr)
	return exitUsage
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: jobq COMMAND [flags] [args]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "commands:")
	for _, c := range commands() {
		fmt.Fprintf(w, "  %-8s %s\n", c.name, c.summary)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "every command takes --store FILE (default $JOBQ_STORE, or %s)\n", defaultStore)
	fmt.Fprintf(w, "job kinds: %s\n", strings.Join(kindNames(), ", "))
	fmt.Fprintf(w, "states: %s\n", queue.StateNames())
}

// newFlags returns a flag set for subcommand name that writes its errors to
// stderr, with the --store flag every command has.
func newFlags(name string, stderr io.Writer) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet("jobq "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	def := os.Getenv("JOBQ_STORE")
	if def == "" {
		def = defaultStore
	}
	path := fs.String("store", def, "the store `file`")
	return fs, path
}

// parse parses args into fs. It returns an exit code and false when the
// command should stop: exitOK for -h, exitUsage for a bad flag.
func parse(fs *flag.FlagSet, args []string) (int, bool) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK, false
		}
		return exitUsage, false
	}
	return 0, true
}

// openQueue opens the store file and returns a queue over it.
func openQueue(path string) (*queue.Queue, error) {
	s, err := store.OpenFile(path)
	if err != nil {
		return nil, err
	}
	return queue.New(s, newClock()), nil
}

// usageErr prints a usage error and returns exitUsage.
func usageErr(stderr io.Writer, format string, args ...any) int {
	fmt.Fprintf(stderr, "jobq: "+format+"\n", args...)
	return exitUsage
}

// fail prints an error and returns exitError.
func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "jobq: %v\n", err)
	return exitError
}

// syncWriter serializes writes from several workers to one stream.
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}
