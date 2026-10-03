package bench

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

// FailuresReport is the markdown report of the uah runs' failures
// (Failures): a summary per harness label, one row per task and label with
// the sums over its runs, every cause, and one row per run.
func FailuresReport(results []Result) string {
	rs := pick(results, func(r Result) bool { return r.Harness == HarnessUAH && r.Failures != nil })
	slices.SortStableFunc(rs, func(a, b Result) int {
		return cmp.Or(cmp.Compare(a.Task, b.Task), cmp.Compare(order(a.Label()), order(b.Label())), cmp.Compare(a.Label(), b.Label()), cmp.Compare(a.Repeat, b.Repeat))
	})
	var b strings.Builder
	fmt.Fprintf(&b, "# Failures report\n\nGenerated %s from %d uah runs. Counts are over the main agent and its subagents; see tools/agentbench/README.md, \"Failures\".\n",
		time.Now().UTC().Format(time.DateTime), len(rs))
	shells := map[string]bool{}
	for _, r := range rs {
		shells[cmp.Or(r.Shell, "(bench env)")] = true
	}
	fmt.Fprintf(&b, "\nHarness shell: %s.\n", strings.Join(slices.Sorted(maps.Keys(shells)), ", "))
	b.WriteString("\n## Summary per harness\n\n")
	b.WriteString("| harness | runs | pass | wall s (median) | requests | input tok | output tok | cache % | " + countHeader + "\n")
	b.WriteString("|---|---:|---:|---:|---:|---:|---:|---:|" + strings.Repeat("---:|", countColumns) + "\n")
	for _, h := range harnesses(rs) {
		g := pick(rs, func(r Result) bool { return r.Label() == h })
		failureRow(&b, h, g)
	}
	b.WriteString("\n## Per task\n\nSums over each task's runs; wall is the median.\n\n")
	b.WriteString("| task | harness | runs | pass | wall s (median) | requests | input tok | output tok | cache % | " + countHeader + "\n")
	b.WriteString("|---|---|---:|---:|---:|---:|---:|---:|---:|" + strings.Repeat("---:|", countColumns) + "\n")
	var tasks []string
	for _, r := range rs {
		if !slices.Contains(tasks, r.Task) {
			tasks = append(tasks, r.Task)
		}
	}
	for _, t := range tasks {
		for _, h := range harnesses(rs) {
			g := pick(rs, func(r Result) bool { return r.Task == t && r.Label() == h })
			if len(g) > 0 {
				failureRow(&b, t+" | "+h, g)
			}
		}
	}
	b.WriteString("\n## Causes\n\nEvery cause of a failed call, per harness.\n\n")
	for _, h := range harnesses(rs) {
		sum := map[string]int{}
		for _, r := range pick(rs, func(r Result) bool { return r.Label() == h }) {
			for c, n := range r.Failures.ByCause {
				sum[c] += n
			}
		}
		causes := slices.SortedFunc(maps.Keys(sum), func(a, b string) int { return cmp.Or(sum[b]-sum[a], cmp.Compare(a, b)) })
		var parts []string
		for _, c := range causes {
			parts = append(parts, fmt.Sprintf("%s %d", c, sum[c]))
		}
		fmt.Fprintf(&b, "- **%s**: %s\n", h, cmp.Or(strings.Join(parts, ", "), "none"))
	}
	b.WriteString("\n## Runs\n\n")
	b.WriteString("| task | harness | # | pass | wall s | requests | input tok | output tok | cache % | " + countHeader + "\n")
	b.WriteString("|---|---|---:|---|---:|---:|---:|---:|---:|" + strings.Repeat("---:|", countColumns) + "\n")
	for _, r := range rs {
		fmt.Fprintf(&b, "| %s | %s | %d | %s | %.0f | %d | %d | %d | %.0f | %s\n", r.Task, r.Label(), r.Repeat, passMark(r),
			sec(r.Metrics.WallMS), r.Metrics.Requests, r.Metrics.Tokens.Input, r.Metrics.Tokens.Output,
			cachePct(r.Metrics.Tokens), counts(sumFailures([]Result{r})))
	}

	return b.String()
}

// The count columns of every table, in counts' order.
const (
	countHeader  = "cmds | failed | fish | go cache | tmp | sandbox | network | other | sh -c % | heredocs | ritual | skill loads | RTK.md | truncated | trunc→retry | re-reads | AGENTS.md | GOCACHE= | TMPDIR= | escalations |"
	countColumns = 20
)

// failureSum is the sums of a group of runs.
type failureSum struct {
	Failures

	ritual, escalations int
}

func sumFailures(rs []Result) failureSum {
	var s failureSum
	s.ByCause = map[string]int{}
	for _, r := range rs {
		f := r.Failures
		s.Commands += f.Commands
		s.Calls += f.Calls
		s.Failed += f.Failed
		s.SubagentFailed += f.SubagentFailed
		for c, n := range f.ByCause {
			s.ByCause[c] += n
		}
		s.Wrapped += f.Wrapped
		s.Heredocs += f.Heredocs
		s.GoCacheOverrides += f.GoCacheOverrides
		s.TmpdirOverrides += f.TmpdirOverrides
		s.Truncated += f.Truncated
		s.TruncatedRetried += f.TruncatedRetried
		s.Rereads += f.Rereads
		s.AgentsLookups += f.AgentsLookups
		s.SkillUses += f.SkillUses
		s.IncludeReads += f.IncludeReads
		s.ritual += r.Metrics.Behavior.RitualRequests
		s.escalations += r.Metrics.Behavior.Escalations
	}

	return s
}

func counts(s failureSum) string {
	wrapped := 0.0
	if s.Commands > 0 {
		wrapped = 100 * float64(s.Wrapped) / float64(s.Commands)
	}
	f := s.Failures

	return fmt.Sprintf("%d | %d | %d | %d | %d | %d | %d | %d | %.0f | %d | %d | %d | %d | %d | %d | %d | %d | %d | %d | %d |",
		f.Commands, f.Failed, f.Cause(CauseFish), f.Cause(CauseGoCache), f.Cause(CauseTmpdir), f.Sandbox(), f.Cause(CauseNetwork), f.Other(),
		wrapped, f.Heredocs, s.ritual, f.SkillUses, f.IncludeReads, f.Truncated, f.TruncatedRetried, f.Rereads, f.AgentsLookups, f.GoCacheOverrides, f.TmpdirOverrides, s.escalations)
}

func failureRow(b *strings.Builder, label string, g []Result) {
	var tok Tokens
	requests, passed := 0, 0
	for _, r := range g {
		tok = tok.Add(r.Metrics.Tokens)
		requests += r.Metrics.Requests
		if r.Passed {
			passed++
		}
	}
	fmt.Fprintf(b, "| %s | %d | %d/%d | %.0f | %d | %d | %d | %.0f | %s\n", label, len(g), passed, len(g),
		median(g, func(m Metrics) float64 { return sec(m.WallMS) }), requests, tok.Input, tok.Output, cachePct(tok), counts(sumFailures(g)))
}

func cachePct(t Tokens) float64 {
	if t.Input == 0 {
		return 0
	}

	return 100 * float64(t.Cached) / float64(t.Input)
}

func passMark(r Result) string {
	switch {
	case r.Passed:
		return "pass"
	case r.Status == StatusDone:
		return "fail"
	default:
		return r.Status
	}
}
