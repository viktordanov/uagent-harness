package embedded

import (
	"time"

	"github.com/viktordanov/unreal-agent/harness/contextbuilder"
	"github.com/viktordanov/unreal-agent/harness/coordinator"
	"github.com/viktordanov/unreal-agent/harness/tool/bash"
)

// wakeHold is how long a turn's results wait for its running calls before
// a call still running wakes the model with its output so far: the longest
// wait of Codex's write_stdin on a running command.
const wakeHold = 5 * time.Minute

// wakePolicy is the coordinator's wake policy: the model is not woken just
// to hear that a call is still running. A turn's results wait until every
// call it issued has finished, or a call has run for wakeHold.
func wakePolicy() coordinator.WakePolicy {
	return coordinator.WakePolicy{Hold: wakeHold, Progress: bash.Progress}
}

// builderOptions choose the runner's preamble: with preamble-wake, the one
// that describes the wake policy; without it, the runner's, which says each
// finished call wakes a turn and a running call shows a placeholder.
func (x experiments) builderOptions() contextbuilder.Options {
	if x.preambleWake {
		return contextbuilder.Options{Hold: wakeHold}
	}

	return contextbuilder.Options{}
}
