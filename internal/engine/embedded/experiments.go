package embedded

import (
	"strings"
	"time"
)

// experimentsEnv names the experiments a process runs, comma-separated.
// An experiment is a switch for an A/B benchmark, not a setting: it has no
// config key, and this is the one place that reads it.
const experimentsEnv = "UAH_EXPERIMENTS"

// experiments are the switches UAH_EXPERIMENTS turns on.
type experiments struct {
	// The wake experiments change when tool results wake the model
	// (wake.go): wake-no-placeholder holds a turn's results until all of its
	// calls finish, wake-debounce gathers results that land within two
	// seconds while calls run, wake-foreground and wake-foreground-long have
	// a Bash call hold the turn up to a yield time, as Codex's exec_command
	// does (wakeForeground, zero when off), and wake-all-done has a turn that
	// issues no calls sleep until every running call ends.
	wakeNoPlaceholder bool
	wakeDebounce      bool
	wakeForeground    time.Duration
	wakeAllDone       bool
}

func readExperiments(getenv func(string) string) experiments {
	var x experiments
	for name := range strings.SplitSeq(getenv(experimentsEnv), ",") {
		switch strings.TrimSpace(name) {
		case "wake-no-placeholder":
			x.wakeNoPlaceholder = true
		case "wake-debounce":
			x.wakeDebounce = true
		case "wake-foreground":
			x.wakeForeground = foregroundYield
		case "wake-foreground-long":
			x.wakeForeground = longForegroundYield
		case "wake-all-done":
			x.wakeAllDone = true
		}
	}

	return x
}
