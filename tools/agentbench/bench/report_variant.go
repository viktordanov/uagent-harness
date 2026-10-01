package bench

import (
	"fmt"
	"slices"
	"strings"
)

// variantTables compares each uah variant with the control (uah without a
// variant), per task and over every run, on what an experiment moves:
// wall time, requests, output tokens, the patch's share of them, and the
// pass rate.
func variantTables(b *strings.Builder, rs []Result) {
	control := pick(rs, func(r Result) bool { return r.Harness == HarnessUAH && r.Variant == "" })
	if len(control) == 0 {
		return
	}
	var variants []string
	for _, r := range rs {
		if r.Harness == HarnessUAH && r.Variant != "" && !slices.Contains(variants, r.Variant) {
			variants = append(variants, r.Variant)
		}
	}
	slices.Sort(variants)
	for _, v := range variants {
		variant := pick(rs, func(r Result) bool { return r.Harness == HarnessUAH && r.Variant == v })
		fmt.Fprintf(b, "\n### uah+%s against uah, per task\n\nMedians per run. A wall ratio below 1 means the variant was faster; patch tokens are the output tokens that wrote patches.\n\n", v)
		b.WriteString("| Task | Passed (ctl / var) | Wall ctl | Wall var | Wall ratio | Requests ctl | Requests var | Output tok. ctl | Output tok. var | Patch tok. ctl | Patch tok. var |\n" +
			"| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
		var tasks []string
		for _, r := range variant {
			if !slices.Contains(tasks, r.Task) {
				tasks = append(tasks, r.Task)
			}
		}
		slices.Sort(tasks)
		for _, t := range tasks {
			onTask := func(r Result) bool { return r.Task == t }
			variantRow(b, t, pick(control, onTask), pick(variant, onTask))
		}
		inTasks := func(r Result) bool { return slices.Contains(tasks, r.Task) }
		variantRow(b, "**all runs**", pick(control, inTasks), variant)
	}
}

func variantRow(b *strings.Builder, name string, ctl, vr []Result) {
	wall := func(m Metrics) float64 { return s(m.WallMS) }
	reqs := func(m Metrics) float64 { return float64(m.Requests) }
	out := func(m Metrics) float64 { return float64(m.Tokens.Output) }
	patch := func(m Metrics) float64 { return float64(m.Behavior.OutputPatch) }
	cw, vw := median(ctl, wall), median(vr, wall)
	ratio := "-"
	if cw > 0 && vw > 0 {
		ratio = fmt.Sprintf("%.2f", vw/cw)
	}
	fmt.Fprintf(b, "| %s | %s / %s | %.1f | %.1f | %s | %.0f | %.0f | %.0f | %.0f | %.0f | %.0f |\n",
		name, passRate(ctl), passRate(vr), cw, vw, ratio, median(ctl, reqs), median(vr, reqs),
		median(ctl, out), median(vr, out), median(ctl, patch), median(vr, patch))
}

func passRate(rs []Result) string {
	if len(rs) == 0 {
		return "-"
	}

	return fmt.Sprintf("%d/%d", passes(rs), len(rs))
}
