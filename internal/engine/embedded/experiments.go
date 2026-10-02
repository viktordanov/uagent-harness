package embedded

import "strings"

// experimentsEnv names the experiments a process runs, comma-separated.
// An experiment is a switch for an A/B benchmark, not a setting: it has no
// config key, and this is the one place that reads it.
const experimentsEnv = "UAH_EXPERIMENTS"

// experiments are the switches UAH_EXPERIMENTS turns on.
type experiments struct {
	// leanRule is the rule Lean mode routes effort by (lean.go), from
	// lean-rule=r0, r1, r2, or r3; the default rule without one.
	leanRule leanRule
}

func readExperiments(getenv func(string) string) experiments {
	x := experiments{leanRule: defaultLeanRule}
	for name := range strings.SplitSeq(getenv(experimentsEnv), ",") {
		if rule, ok := strings.CutPrefix(strings.TrimSpace(name), "lean-rule="); ok {
			if r, ok := parseLeanRule(rule); ok {
				x.leanRule = r
			}
		}
	}

	return x
}
