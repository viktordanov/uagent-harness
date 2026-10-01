package embedded

import "strings"

// experimentsEnv names the experiments a process runs, comma-separated.
// An experiment is a switch for an A/B benchmark, not a setting: it has no
// config key, and this is the one place that reads it.
const experimentsEnv = "UAH_EXPERIMENTS"

// experiments are the switches UAH_EXPERIMENTS turns on.
type experiments struct {
	// freeformPatch offers apply_patch as Codex does, as a custom tool whose
	// input is the raw patch, sampled from Codex's Lark grammar, instead of
	// a function tool with the patch JSON-escaped in "input".
	freeformPatch bool
}

func readExperiments(getenv func(string) string) experiments {
	var x experiments
	for name := range strings.SplitSeq(getenv(experimentsEnv), ",") {
		if strings.TrimSpace(name) == "freeform-patch" {
			x.freeformPatch = true
		}
	}

	return x
}
