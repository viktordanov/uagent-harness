package embedded

import "strings"

// experimentsEnv names the experiments a process runs, comma-separated.
// An experiment is a switch for an A/B benchmark, not a setting: it has no
// config key, and this is the one place that reads it.
const experimentsEnv = "UAH_EXPERIMENTS"

// experiments are the switches UAH_EXPERIMENTS turns on.
type experiments struct {
	// wakeHold60s has a call still running wake the model after a minute,
	// not five (wake.go).
	wakeHold60s bool
	// wakeReleaseQuick releases the results that are in ten seconds after a
	// turn while its long calls still run, so the model can work beside them
	// (wake.go).
	wakeReleaseQuick bool
	// autoVerify runs a quick project check after each patch that applies
	// and adds its result to the patch's output (autoverify.go).
	autoVerify bool
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
		case "wake-hold-60s":
			x.wakeHold60s = true
		case "wake-release-quick":
			x.wakeReleaseQuick = true
		case "auto-verify":
			x.autoVerify = true
		case "primed-first-turn":
			x.primedFirstTurn = true
		case "effort-by-turn":
			x.effortByTurn = true
		}
	}

	return x
}
