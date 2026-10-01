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
}

func readExperiments(getenv func(string) string) experiments {
	var x experiments
	for name := range strings.SplitSeq(getenv(experimentsEnv), ",") {
		switch strings.TrimSpace(name) {
		case "wake-hold-60s":
			x.wakeHold60s = true
		case "wake-release-quick":
			x.wakeReleaseQuick = true
		}
	}

	return x
}
