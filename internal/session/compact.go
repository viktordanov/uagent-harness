package session

import (
	"errors"
	"path/filepath"
	"slices"
	"time"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/compaction"
	"github.com/viktordanov/uagent-harness/internal/engine"
)

// ErrNoCompaction means the engine cannot compact the context.
var ErrNoCompaction = errors.New("compaction needs the embedded engine")

type cmdCompact struct{}

// Compact summarizes the context, keeping every user message verbatim: before
// the live run's next model request, or before the next run's first one when
// the session is idle. The engine reports it with engine.CompactionStarted
// and engine.Compacted.
func (s *Session) Compact() error {
	_, err := call[struct{}](s, cmdCompact{})

	return err
}

func (s *Session) onCompact() error {
	if !s.caps.Compaction {
		return ErrNoCompaction
	}
	s.compactPending = true
	if s.state == StateRunning && s.run != nil && s.run.Compact() == nil {
		s.emit(Notice{At: time.Now(), Level: LevelInfo, Message: "Compacting the context before the next model request"})

		return nil
	}
	s.emit(Notice{At: time.Now(), Level: LevelInfo, Message: "The context will be compacted before the next message"})

	return nil
}

// noteCompaction clears a pending /compact once the engine starts it, so a
// request that a run could not serve moves on to the next run.
func (s *Session) noteCompaction(e core.Event) {
	if v, ok := e.(engine.CompactionStarted); ok && v.Trigger == compaction.TriggerManual {
		s.compactPending = false
	}
}

// withCompactions adds the session's saved compactions to its loaded runs as
// engine.Compacted events, each in the run it happened in and in time order,
// so a reloaded transcript shows them. The runner's events.jsonl stays as
// the runner wrote it; the compaction log is the source.
func withCompactions(stateDir, id string, runs []LoadedRun) ([]LoadedRun, error) {
	if len(runs) == 0 {
		return runs, nil
	}
	records, _, err := compaction.OpenLog(filepath.Join(stateDir, "sessions"), id).Records()
	if err != nil {
		return nil, err
	}
	for _, rec := range records {
		i := len(runs) - 1
		for i > 0 && runs[i].Record.Result.StartedAt.After(rec.At) {
			i--
		}
		ev := engine.Compacted{At: rec.At, Trigger: rec.Trigger, Summary: rec.Summary}
		events := runs[i].Events
		at := slices.IndexFunc(events, func(e core.Event) bool { return e.OccurredAt().After(rec.At) })
		if at < 0 {
			at = len(events)
		}
		runs[i].Events = slices.Insert(events, at, core.Event(ev))
	}

	return runs, nil
}
