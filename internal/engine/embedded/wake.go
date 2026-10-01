package embedded

import (
	"time"

	"github.com/viktordanov/unreal-agent/harness/coordinator"
	"github.com/viktordanov/unreal-agent/harness/tool/bash"
)

const (
	// wakeHold is how long a turn's results wait for its running calls
	// before a call still running wakes the model with its output so far:
	// the longest wait of Codex's write_stdin on a running command.
	wakeHold = 5 * time.Minute
	// shortWakeHold is wake-hold-60s's.
	shortWakeHold = time.Minute
	// releaseQuick is when wake-release-quick releases the results that are
	// in while long calls still run.
	releaseQuick = 10 * time.Second
)

// wakePolicy is the coordinator's wake policy: the model is not woken just
// to hear that a call is still running. A turn's results wait until every
// call it issued has finished, or a call has run for wakeHold.
func (x experiments) wakePolicy() coordinator.WakePolicy {
	p := coordinator.WakePolicy{Hold: wakeHold, Progress: bash.Progress}
	if x.wakeHold60s {
		p.Hold = shortWakeHold
	}
	if x.wakeReleaseQuick {
		p.ReleaseQuick = releaseQuick
	}

	return p
}
