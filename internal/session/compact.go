package session

import (
	"errors"
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
