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

	"github.com/viktordanov/uagent-harness/internal/engine"
)

// withPatches adds the diff of each applied apply_patch call to its run as
// engine.PatchApplied, after the call's ToolFinished, so a reloaded
// transcript shows it. The diff comes from the call's completed job in the
// run's events file, the same item the live engine read it from.
func withPatches(runs []LoadedRun) []LoadedRun {
	for i := range runs {
		for _, p := range runPatches(runs[i].Record.Dir) {
			events := runs[i].Events
			at := slices.IndexFunc(events, func(e core.Event) bool {
				f, ok := e.(core.ToolFinished)

				return ok && f.CallID == p.CallID
			}) + 1
			if at == 0 {
				at = slices.IndexFunc(events, func(e core.Event) bool { return e.OccurredAt().After(p.At) })
				if at < 0 {
					at = len(events)
				}
			}
			runs[i].Events = slices.Insert(events, at, core.Event(p))
		}
	}

	return runs
}

// runPatches reads a run's applied patches, once per call; an unreadable
// file has none.
func runPatches(runDir string) []engine.PatchApplied {
	f, err := os.Open(filepath.Join(runDir, harness.EventsFile))
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []engine.PatchApplied
	seen := map[string]bool{}
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if p, ok := engine.PatchFromItem(line); ok && !seen[p.CallID] {
			seen[p.CallID] = true
			out = append(out, p)
		}
		if errors.Is(err, io.EOF) || err != nil {
			return out
		}
	}
}
