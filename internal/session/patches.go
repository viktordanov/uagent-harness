package session

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/viktordanov/uagent/core"
	"github.com/viktordanov/uagent/harness"

	"github.com/viktordanov/uah/internal/engine"
)

// withPatches adds the diff of each applied apply_patch call to its run as
// engine.PatchApplied, and the output of each failed command and finished
// MCP call as engine.ToolOutput, after the call's ToolFinished, so a
// reloaded transcript shows them. Both come from the call's finished
// operation in the run's events file, the same item the live engine read
// them from.
func withPatches(runs []LoadedRun) []LoadedRun {
	for i := range runs {
		for _, p := range runCallEvents(runs[i].Record.Dir) {
			runs[i].Events = insertAfterCall(runs[i].Events, p)
		}
	}

	return runs
}

// insertAfterCall puts ev after its call's ToolFinished, or else in time
// order.
func insertAfterCall(events []core.Event, ev callEvent) []core.Event {
	at := slices.IndexFunc(events, func(e core.Event) bool {
		f, ok := e.(core.ToolFinished)

		return ok && f.CallID == ev.callID
	}) + 1
	if at == 0 {
		at = slices.IndexFunc(events, func(e core.Event) bool { return e.OccurredAt().After(ev.OccurredAt()) })
		if at < 0 {
			at = len(events)
		}
	}

	return slices.Insert(events, at, ev.Event)
}

// callEvent is an event about one call.
type callEvent struct {
	core.Event

	callID string
}

// runCallEvents reads a run's applied patches and tool outputs, once per
// call each; an unreadable file has none.
func runCallEvents(runDir string) []callEvent {
	f, err := os.Open(filepath.Join(runDir, harness.EventsFile))
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []callEvent
	patched, output := map[string]bool{}, map[string]bool{}
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if p, ok := engine.PatchFromItem(line); ok && !patched[p.CallID] {
			patched[p.CallID] = true
			out = append(out, callEvent{p, p.CallID})
		}
		if o, ok := engine.ToolOutputFromItem(line); ok && !output[o.CallID] {
			output[o.CallID] = true
			out = append(out, callEvent{o, o.CallID})
		}
		if errors.Is(err, io.EOF) || err != nil {
			return out
		}
	}
}
