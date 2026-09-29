package eval

import (
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

// Aggregate sums one strategy's results over the cases.
type Aggregate struct {
	Strategy string
	// Opaque strategies keep facts the rules cannot read (an encrypted
	// item), so their recall is not reported.
	Opaque bool
	Cases  int
	// Medians over the cases.
	After, Ratio, Headroom, CacheMiss, CallInput float64
	// Pooled over the cases.
	ByActor                       map[Actor]int64
	Recall                        map[Kind]Count
	UserKept, SkillsKept, Refetch Count
}

// Summarize aggregates a strategy's results.
func Summarize(strategy string, opaque bool, results []Result) Aggregate {
	a := Aggregate{Strategy: strategy, Opaque: opaque, Cases: len(results), ByActor: map[Actor]int64{}, Recall: map[Kind]Count{}}
	var after, ratio, headroom, miss, call []float64
	for _, r := range results {
		after = append(after, float64(r.After))
		if r.Before > 0 {
			ratio = append(ratio, float64(r.After)/float64(r.Before))
		}
		headroom = append(headroom, float64(r.Headroom))
		miss = append(miss, float64(r.CacheMiss))
		call = append(call, float64(r.CallInput))
		for k, v := range r.ByActor {
			a.ByActor[k] += v
		}
		for k, v := range r.Recall {
			a.Recall[k] = a.Recall[k].Add(v)
		}
		a.UserKept, a.SkillsKept, a.Refetch = a.UserKept.Add(r.UserKept), a.SkillsKept.Add(r.SkillsKept), a.Refetch.Add(r.Refetch)
	}
	a.After, a.Ratio, a.Headroom, a.CacheMiss, a.CallInput = median(after), median(ratio), median(headroom), median(miss), median(call)

	return a
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := slices.Sorted(slices.Values(xs))
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}

	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

// Write prints the aggregates as Markdown tables: tokens, what stays per
// actor, and facts. It prints numbers only, never request content.
func Write(w io.Writer, rows []Aggregate) {
	fmt.Fprintln(w, "## Tokens (medians over the cases)")
	fmt.Fprintln(w)
	table(w, []string{colStrategy, "cases", "tokens after", "after/before", "headroom", "uncached first request", "summary call input"}, rows, func(a Aggregate) []string {
		return []string{a.Strategy, strconv.Itoa(a.Cases), num(a.After), fmt.Sprintf("%.3f", a.Ratio), num(a.Headroom), num(a.CacheMiss), num(a.CallInput)}
	})
	fmt.Fprintln(w, "\n## Tokens after, per actor (pooled)")
	fmt.Fprintln(w)
	head := []string{colStrategy}
	for _, act := range Actors {
		head = append(head, string(act))
	}
	table(w, head, rows, func(a Aggregate) []string {
		row := []string{a.Strategy}
		for _, act := range Actors {
			row = append(row, num(float64(a.ByActor[act])))
		}

		return row
	})
	fmt.Fprintln(w, "\n## Facts kept (pooled; re-fetch: later calls that read again what was dropped, lower is better)")
	fmt.Fprintln(w)
	head = []string{colStrategy}
	for _, k := range Kinds {
		head = append(head, string(k))
	}
	head = append(head, "user messages", "skill bodies", "re-fetch")
	table(w, head, rows, func(a Aggregate) []string {
		row := []string{a.Strategy}
		for _, k := range Kinds {
			row = append(row, share(a.Recall[k], a.Opaque))
		}

		return append(row, share(a.UserKept, false), share(a.SkillsKept, a.Opaque), share(a.Refetch, false))
	})
}

const colStrategy = "strategy"

func table(w io.Writer, head []string, rows []Aggregate, cells func(Aggregate) []string) {
	fmt.Fprintln(w, "| "+strings.Join(head, " | ")+" |")
	fmt.Fprintln(w, "|"+strings.Repeat(" --- |", len(head)))
	for _, r := range rows {
		fmt.Fprintln(w, "| "+strings.Join(cells(r), " | ")+" |")
	}
}

func share(c Count, opaque bool) string {
	switch {
	case opaque:
		return "n/a"
	case c.Total == 0:
		return "-"
	}

	return fmt.Sprintf("%.0f%% of %d", 100*c.Share(), c.Total)
}

func num(f float64) string {
	n := int64(f + 0.5)
	s := strconv.FormatInt(n, 10)
	for i := len(s) - 3; i > 0 && s[i-1] != '-'; i -= 3 {
		s = s[:i] + "," + s[i:]
	}

	return s
}
