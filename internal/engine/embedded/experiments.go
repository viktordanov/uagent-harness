package embedded

import "strings"

// experimentsEnv names the experiments a process runs, comma-separated.
// An experiment is a switch for an A/B benchmark, not a setting: it has no
// config key, and this is the one place that reads it.
const experimentsEnv = "UAH_EXPERIMENTS"

// experiments are the switches UAH_EXPERIMENTS turns on.
type experiments struct {
	// preambleWake gives the runner's preamble the wake policy's words: a
	// turn's results arrive together, and a call still running after the
	// hold wakes the model with its output so far (wake.go).
	preambleWake bool
	// primedFirstTurn gives a new session's first request a compact
	// workspace context: the files, git status, and the AGENTS.md
	// includes (primed.go).
	primedFirstTurn bool
	// effortByTurn sends a request that only continues after tool results
	// one effort level lower (effortturn.go).
	effortByTurn bool
}

func readExperiments(getenv func(string) string) experiments {
	var x experiments
	for name := range strings.SplitSeq(getenv(experimentsEnv), ",") {
		switch strings.TrimSpace(name) {
		case "preamble-wake":
			x.preambleWake = true
		case "primed-first-turn":
			x.primedFirstTurn = true
		case "effort-by-turn":
			x.effortByTurn = true
		}
	}

	return x
}
