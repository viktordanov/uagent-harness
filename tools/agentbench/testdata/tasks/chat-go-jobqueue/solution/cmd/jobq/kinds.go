package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"time"

	"example.com/jobq/queue"
	"example.com/jobq/worker"
)

// kind is a built-in kind of job: how jobq add checks its payload, and the
// handler jobq run gives it.
type kind struct {
	help     string
	validate func(payload string) error
	handler  func(out io.Writer) worker.Handler
}

// kinds are the built-in kinds of job.
var kinds = map[string]kind{
	"echo": {
		help:     "prints \"ID: PAYLOAD\" and succeeds",
		validate: func(string) error { return nil },
		handler: func(out io.Writer) worker.Handler {
			return func(_ context.Context, t queue.Job) error {
				_, err := fmt.Fprintf(out, "%s: %s\n", t.ID, t.Payload)
				return err
			}
		},
	},
	"fail": {
		help:     "fails every attempt",
		validate: func(string) error { return nil },
		handler: func(io.Writer) worker.Handler {
			return func(context.Context, queue.Job) error {
				return errors.New("fail: this job always fails")
			}
		},
	},
	"flaky": {
		help:     "fails its first N attempts (PAYLOAD is N), then succeeds",
		validate: func(p string) error { _, err := flakyCount(p); return err },
		handler: func(io.Writer) worker.Handler {
			return func(_ context.Context, t queue.Job) error {
				n, err := flakyCount(t.Payload)
				if err != nil {
					return err
				}
				if t.Attempts <= n {
					return fmt.Errorf("flaky: attempt %d of the %d that fail", t.Attempts, n)
				}
				return nil
			}
		},
	},
	"sleep": {
		help: "waits for PAYLOAD, a duration such as 250ms, then succeeds",
		validate: func(p string) error {
			_, err := sleepFor(p)
			return err
		},
		handler: func(io.Writer) worker.Handler {
			return func(ctx context.Context, t queue.Job) error {
				d, err := sleepFor(t.Payload)
				if err != nil {
					return err
				}
				timer := time.NewTimer(d)
				defer timer.Stop()
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-timer.C:
					return nil
				}
			}
		},
	},
}

func flakyCount(p string) (int, error) {
	n, err := strconv.Atoi(p)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("flaky: payload %q is not a count of attempts", p)
	}
	return n, nil
}

func sleepFor(p string) (time.Duration, error) {
	d, err := time.ParseDuration(p)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("sleep: payload %q is not a duration", p)
	}
	return d, nil
}

// kindNames returns the built-in kinds, sorted.
func kindNames() []string {
	names := make([]string, 0, len(kinds))
	for k := range kinds {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// registry returns a registry of every built-in kind, writing their output to
// out.
func registry(out io.Writer) *worker.Registry {
	reg := worker.NewRegistry()
	for _, name := range kindNames() {
		reg.Register(name, kinds[name].handler(out))
	}
	return reg
}
