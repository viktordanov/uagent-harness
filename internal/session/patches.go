package session

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/viktordanov/uagent/core"
	"github.com/viktordanov/uagent/harness"

	"github.com/viktordanov/uah/internal/engine"
)

// loadEvents reads a run's events, as harness.LoadEvents does, and adds
// the diff of each applied apply_patch call as engine.PatchApplied, and
// the output of each failed command and finished MCP call as
// engine.ToolOutput, after the call's ToolFinished, so a reloaded
// transcript shows them. Both come from the call's finished operation in
// the same file, the item the live engine read them from, so the file is
// read once. br is reset to the file, so runs share its buffer.
func loadEvents(br *bufio.Reader, runDir string) ([]core.Event, error) {
	f, err := os.Open(filepath.Join(runDir, harness.EventsFile))
	if err != nil {
		return nil, fmt.Errorf("failed to open events: %w", err)
	}
	defer f.Close()
	br.Reset(f)
	decoder := harness.NewDecoder()
	var events []core.Event
	var calls []callEvent
	patched, output := map[string]bool{}, map[string]bool{}
	for {
		line, err := br.ReadBytes('\n')
		events = append(events, decoder.Decode(line)...)
		if p, ok := engine.PatchFromItem(line); ok && !patched[p.CallID] {
			patched[p.CallID] = true
			calls = append(calls, callEvent{p, p.CallID})
		}
		if o, ok := engine.ToolOutputFromItem(line); ok && !output[o.CallID] {
			output[o.CallID] = true
			calls = append(calls, callEvent{o, o.CallID})
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read events: %w", err)
		}
	}
	for _, c := range calls {
		events = insertAfterCall(events, c)
	}

	return events, nil
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
