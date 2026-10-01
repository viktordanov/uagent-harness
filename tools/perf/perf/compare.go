package perf

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"text/tabwriter"
)

// Change is one metric of one scenario in two reports.
type Change struct {
	Scenario, Metric string
	Old, New         float64
	// Regression and Improvement are set when the change passes both the
	// relative threshold and the metric's floor.
	Regression, Improvement bool
}

// floors are the smallest absolute changes that count, by metric name or
// suffix, so noise on small numbers never fails a comparison.
var floors = []struct {
	match string
	floor float64
}{
	{metricGoroutinesLeft, 3},
	{metricConnsAfter, 1},
	{"peak_goroutines", 10},
	{"peak_conns", 2},
	{metricAllocs, 20_000},
	{"wakeups", 200},
	{"requests", 1},
	{"_ms", 5},
	{"_mb", 1},
	{"_kb", 64},
	{"_per_s", 1},
}

// skipped are metrics that describe the run rather than cost, or depend
// on the scenarios before (goroutines before and after; goroutines_left
// is compared): never compared.
var skipped = []string{metricGoroutinesBefore, metricGoroutinesAfter, "events", "updates", "views", "term_writes", "events_per_s", "request_mb", "server_ms"}

func floorOf(metric string) float64 {
	for _, f := range floors {
		if metric == f.match || (strings.HasPrefix(f.match, "_") && strings.HasSuffix(metric, f.match)) {
			return f.floor
		}
	}

	return 0
}

// Compare lists the metrics of the scenarios in both reports; a metric is
// a regression when it grew by more than threshold (0.25 is 25%) and by
// more than its floor, an improvement when it shrank as much.
func Compare(old, cur *Report, threshold float64) []Change {
	var changes []Change
	for _, r := range cur.Results {
		base, ok := old.Result(r.Name)
		if !ok {
			continue
		}
		for _, k := range slices.Sorted(maps.Keys(r.Metrics)) {
			was, found := base.Metrics[k]
			if !found || slices.Contains(skipped, k) {
				continue
			}
			c := Change{Scenario: r.Name, Metric: k, Old: was, New: r.Metrics[k]}
			delta, floor := c.New-c.Old, floorOf(k)
			c.Regression = delta > floor && delta > threshold*c.Old
			c.Improvement = -delta > floor && -delta > threshold*c.Old
			changes = append(changes, c)
		}
	}

	return changes
}

// Regressions counts the regressions in changes.
func Regressions(changes []Change) int {
	n := 0
	for _, c := range changes {
		if c.Regression {
			n++
		}
	}

	return n
}

// WriteChanges prints the regressions and improvements (every change with
// all).
func WriteChanges(w io.Writer, changes []Change, all bool) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(tw, "scenario\tmetric\tbaseline\tnow\tchange\t\t")
	shown := 0
	for _, c := range changes {
		if !all && !c.Regression && !c.Improvement {
			continue
		}
		shown++
		mark := ""
		switch {
		case c.Regression:
			mark = "REGRESSION"
		case c.Improvement:
			mark = "better"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t\n", c.Scenario, c.Metric, format(c.Metric, c.Old), format(c.Metric, c.New), percent(c.Old, c.New), mark)
	}
	tw.Flush() //nolint:errcheck // printing
	if shown == 0 {
		fmt.Fprintln(w, "no change beyond the threshold")
	}
}

func percent(old, cur float64) string {
	if old == 0 {
		if cur == 0 {
			return "0%"
		}

		return "new"
	}

	return fmt.Sprintf("%+.0f%%", (cur-old)/old*100)
}
