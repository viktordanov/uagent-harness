package engine

import (
	"time"

	"github.com/viktordanov/uagent-harness/internal/compaction"
)

// Engine events join a run's events.

// CompactionStarted means the engine is summarizing the context.
type CompactionStarted struct {
	At      time.Time
	Trigger compaction.Trigger
	// Tokens is the context in use that the last response reported.
	Tokens int64
}

// Compacted means a compaction finished. Err is empty on success; on failure
// the request went out uncompacted.
type Compacted struct {
	At      time.Time
	Trigger compaction.Trigger
	Summary string
	Err     string
}

func (e CompactionStarted) OccurredAt() time.Time { return e.At }
func (e Compacted) OccurredAt() time.Time         { return e.At }
