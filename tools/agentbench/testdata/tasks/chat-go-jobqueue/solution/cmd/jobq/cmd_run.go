package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"example.com/jobq/metrics"
	"example.com/jobq/retry"
	"example.com/jobq/worker"
)

// cmdRun is jobq run [--store F] [--workers N] [--max-attempts N]
// [--max-backoff D] [--poll D] [--timeout D] [--job-timeout D] [--quiet]
// [--table].
//
// It first puts back to pending any job a previous run left running, then
// runs a worker pool until no job is pending or running. Job output (from
// echo) goes to stdout, one line per failed attempt to stderr, and a summary
// line to stdout at the end. The exit code is 1 when a job went dead in this
// run.
func cmdRun(args []string, stdout, stderr io.Writer) int {
	fs, path := newFlags("run", stderr)
	workers := fs.Int("workers", 4, "run `n` jobs at a time")
	maxAttempts := fs.Int("max-attempts", retry.DefaultMaxAttempts, "attempt a job at most `n` times, the first one included")
	maxBackoff := fs.Duration("max-backoff", retry.DefaultMaxBackoff, "cap the wait before a retry, jitter included, at this `duration`")
	poll := fs.Duration("poll", worker.DefaultPoll, "how often an idle worker looks for a ready job")
	timeout := fs.Duration("timeout", 0, "stop the run after this `duration` (0: no limit)")
	jobTimeout := fs.Duration("job-timeout", 0, "fail an attempt that takes longer than this `duration` (0: no limit)")
	quiet := fs.Bool("quiet", false, "do not log failed attempts")
	table := fs.Bool("table", false, "print a table of counts per kind after the summary")
	if code, ok := parse(fs, args); !ok {
		return code
	}
	if fs.NArg() != 0 {
		return usageErr(stderr, "run takes no arguments")
	}
	if *workers < 1 {
		return usageErr(stderr, "--workers must be at least 1, not %d", *workers)
	}
	if *maxAttempts < 1 {
		return usageErr(stderr, "--max-attempts must be at least 1, not %d", *maxAttempts)
	}
	if *maxBackoff <= 0 {
		return usageErr(stderr, "--max-backoff must be positive, not %s", *maxBackoff)
	}
	if *poll <= 0 {
		return usageErr(stderr, "--poll must be positive, not %s", *poll)
	}
	if *timeout < 0 || *jobTimeout < 0 {
		return usageErr(stderr, "a timeout cannot be negative")
	}

	q, err := openQueue(*path)
	if err != nil {
		return fail(stderr, err)
	}
	out := &syncWriter{w: stdout}
	errw := &syncWriter{w: stderr}
	if n, err := q.RecoverRunning(); err != nil {
		return fail(stderr, err)
	} else if n > 0 {
		fmt.Fprintf(errw, "jobq: %d interrupted jobs put back to pending\n", n)
	}

	policy := retry.Default()
	policy.MaxAttempts = *maxAttempts
	policy.MaxBackoff = *maxBackoff
	counters := metrics.New()
	cfg := worker.Config{
		Workers:    *workers,
		Policy:     policy,
		Poll:       *poll,
		JobTimeout: *jobTimeout,
		Metrics:    counters,
	}
	if !*quiet {
		cfg.Logf = func(format string, args ...any) {
			fmt.Fprintf(errw, "jobq: "+format+"\n", args...)
		}
	}

	ctx := context.Background()
	if *timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *timeout)
		defer cancel()
	}
	runErr := worker.New(q, registry(out), cfg).Run(ctx)

	snap := counters.Snapshot()
	fmt.Fprintf(out, "run: %s\n", snap.Summary())
	if *table {
		if err := snap.WriteTable(out); err != nil {
			return fail(stderr, err)
		}
	}
	switch {
	case errors.Is(runErr, context.DeadlineExceeded):
		return fail(errw, fmt.Errorf("run stopped after %s with jobs left", *timeout))
	case runErr != nil:
		return fail(errw, runErr)
	case snap.Failed > 0:
		return fail(errw, fmt.Errorf("%d jobs failed and went dead", snap.Failed))
	}
	return exitOK
}
