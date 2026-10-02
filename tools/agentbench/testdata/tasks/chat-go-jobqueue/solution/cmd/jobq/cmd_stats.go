package main

import (
	"encoding/json"
	"fmt"
	"io"

	"example.com/jobq/queue"
)

// stats is the output of jobq stats, one field per state, in the order a job
// passes through them.
type stats struct {
	Pending int `json:"pending"`
	Running int `json:"running"`
	Done    int `json:"done"`
	Dead    int `json:"dead"`
}

// cmdStats is jobq stats [--store F]. It prints the number of jobs in each
// state as one JSON object: {"pending":N,"running":N,"done":N,"dead":N}.
func cmdStats(args []string, stdout, stderr io.Writer) int {
	fs, path := newFlags("stats", stderr)
	if code, ok := parse(fs, args); !ok {
		return code
	}
	if fs.NArg() != 0 {
		return usageErr(stderr, "stats takes no arguments")
	}
	q, err := openQueue(*path)
	if err != nil {
		return fail(stderr, err)
	}
	counts, err := q.Store().Counts()
	if err != nil {
		return fail(stderr, err)
	}
	b, err := json.Marshal(stats{
		Pending: counts[queue.Pending],
		Running: counts[queue.Running],
		Done:    counts[queue.Done],
		Dead:    counts[queue.Dead],
	})
	if err != nil {
		return fail(stderr, err)
	}
	fmt.Fprintln(stdout, string(b))
	return exitOK
}
