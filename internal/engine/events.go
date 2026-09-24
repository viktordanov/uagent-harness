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
// the request went out uncompacted. Interrupted means the user's interrupt
// or the run's end stopped it (Err says so too). Warning, on success, is
// what the user should know, such as a context still above the automatic
// limit.
type Compacted struct {
	At          time.Time
	Trigger     compaction.Trigger
	Summary     string
	Err         string
	Interrupted bool
	Warning     string
}

func (e CompactionStarted) OccurredAt() time.Time { return e.At }
func (e Compacted) OccurredAt() time.Time         { return e.At }

// AutoReviewed reports the auto-reviewer's verdict on an action that needed
// approval. Outcome is allow, deny, or ask_user (the user decides).
type AutoReviewed struct {
	At      time.Time
	Command string
	Outcome string
	Risk    string
	Reason  string
}

func (e AutoReviewed) OccurredAt() time.Time { return e.At }

// Reconnecting means a model request failed, for a lost connection or an
// HTTP status the client retries, and the client tries again: attempt
// Attempt of MaxAttempts starts after about Delay. Reason is the failure.
// Only the embedded engine reports it (Capabilities.Reconnect).
type Reconnecting struct {
	At          time.Time
	Attempt     int
	MaxAttempts int
	Delay       time.Duration
	Reason      string
}

// ReconnectEnded means a model request that was retried stopped retrying:
// OK when a response arrived, otherwise it failed or was canceled.
type ReconnectEnded struct {
	At time.Time
	OK bool
}

func (e Reconnecting) OccurredAt() time.Time   { return e.At }
func (e ReconnectEnded) OccurredAt() time.Time { return e.At }
