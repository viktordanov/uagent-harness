package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"example.com/jobq/queue"
)

// cmdAdd is jobq add [--store F] [--delay D] KIND [PAYLOAD]. It prints the new
// job's ID.
func cmdAdd(args []string, stdout, stderr io.Writer) int {
	fs, path := newFlags("add", stderr)
	delay := fs.Duration("delay", 0, "make the job ready only after this `duration`")
	if code, ok := parse(fs, args); !ok {
		return code
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		return usageErr(stderr, "add wants KIND [PAYLOAD]")
	}
	name, payload := fs.Arg(0), fs.Arg(1)
	k, ok := kinds[name]
	if !ok {
		return usageErr(stderr, "unknown kind %q (want one of %s)", name, strings.Join(kindNames(), ", "))
	}
	if err := k.validate(payload); err != nil {
		return usageErr(stderr, "%v", err)
	}
	if *delay < 0 {
		return usageErr(stderr, "negative --delay %s", *delay)
	}
	q, err := openQueue(*path)
	if err != nil {
		return fail(stderr, err)
	}
	t, err := q.Enqueue(name, payload, *delay)
	if err != nil {
		return fail(stderr, err)
	}
	fmt.Fprintln(stdout, t.ID)
	return exitOK
}

// cmdList is jobq list [--store F] [--state S] [--json]. It prints one line
// per job, oldest first: ID, state, kind, attempts, payload, last error.
func cmdList(args []string, stdout, stderr io.Writer) int {
	fs, path := newFlags("list", stderr)
	stateName := fs.String("state", "", "list only the jobs in this `state`")
	asJSON := fs.Bool("json", false, "print a JSON array")
	if code, ok := parse(fs, args); !ok {
		return code
	}
	if fs.NArg() != 0 {
		return usageErr(stderr, "list takes no arguments")
	}
	var state queue.State
	if *stateName != "" {
		st, err := queue.ParseState(*stateName)
		if err != nil {
			return usageErr(stderr, "%v", err)
		}
		state = st
	}
	q, err := openQueue(*path)
	if err != nil {
		return fail(stderr, err)
	}
	tasks, err := q.List(state)
	if err != nil {
		return fail(stderr, err)
	}
	if *asJSON {
		if tasks == nil {
			tasks = []queue.Job{}
		}
		return writeJSON(stdout, stderr, tasks)
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	for _, t := range tasks {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\n", t.ID, t.State, t.Kind, t.Attempts, quote(t.Payload), t.LastError)
	}
	if err := tw.Flush(); err != nil {
		return fail(stderr, err)
	}
	return exitOK
}

// cmdShow is jobq show [--store F] ID.
func cmdShow(args []string, stdout, stderr io.Writer) int {
	fs, path := newFlags("show", stderr)
	if code, ok := parse(fs, args); !ok {
		return code
	}
	if fs.NArg() != 1 {
		return usageErr(stderr, "show wants one job ID")
	}
	q, err := openQueue(*path)
	if err != nil {
		return fail(stderr, err)
	}
	t, err := q.Get(fs.Arg(0))
	if err != nil {
		return fail(stderr, err)
	}
	return writeJSON(stdout, stderr, t)
}

// cmdRequeue is jobq requeue [--store F] ID... It puts dead jobs back to
// pending with their attempts reset.
func cmdRequeue(args []string, stdout, stderr io.Writer) int {
	fs, path := newFlags("requeue", stderr)
	if code, ok := parse(fs, args); !ok {
		return code
	}
	if fs.NArg() == 0 {
		return usageErr(stderr, "requeue wants at least one job ID")
	}
	q, err := openQueue(*path)
	if err != nil {
		return fail(stderr, err)
	}
	code := exitOK
	for _, id := range fs.Args() {
		t, err := q.Requeue(id)
		if err != nil {
			code = fail(stderr, err)
			continue
		}
		fmt.Fprintf(stdout, "%s requeued\n", t.ID)
	}
	return code
}

// cmdPurge is jobq purge [--store F] [--older-than D] --state S. It deletes
// the jobs in a terminal state, and prints how many.
func cmdPurge(args []string, stdout, stderr io.Writer) int {
	fs, path := newFlags("purge", stderr)
	stateName := fs.String("state", "", "delete the jobs in this `state` (required)")
	olderThan := fs.Duration("older-than", 0, "delete only jobs last changed longer ago than this `duration`")
	if code, ok := parse(fs, args); !ok {
		return code
	}
	if fs.NArg() != 0 {
		return usageErr(stderr, "purge takes no arguments")
	}
	if *stateName == "" {
		return usageErr(stderr, "purge needs --state")
	}
	state, err := queue.ParseState(*stateName)
	if err != nil {
		return usageErr(stderr, "%v", err)
	}
	if !state.Terminal() {
		return usageErr(stderr, "purge only deletes finished jobs, not %s ones", state)
	}
	q, err := openQueue(*path)
	if err != nil {
		return fail(stderr, err)
	}
	tasks, err := q.List(state)
	if err != nil {
		return fail(stderr, err)
	}
	now := q.Clock().Now()
	n := 0
	for _, t := range tasks {
		if *olderThan > 0 && now.Sub(t.UpdatedAt) < *olderThan {
			continue
		}
		if err := q.Store().Delete(t.ID); err != nil {
			return fail(stderr, err)
		}
		n++
	}
	fmt.Fprintf(stdout, "purged %d %s jobs\n", n, state)
	return exitOK
}

func writeJSON(stdout, stderr io.Writer, v any) int {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fail(stderr, err)
	}
	return exitOK
}

// quote shows an empty payload as "-" and one with white space quoted, so the
// columns of list stay aligned.
func quote(s string) string {
	if s == "" {
		return "-"
	}
	if strings.ContainsAny(s, " \t\r\n") {
		return fmt.Sprintf("%q", s)
	}
	return s
}
