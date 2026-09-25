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

// Rewound means the session went back to before the message MessageID
// (Rewinder): that message and everything after it left the model's
// context, and the session file keeps them. Tokens is the context in use
// that the last response before the message reported (0: unknown).
type Rewound struct {
	At        time.Time
	MessageID string
	Tokens    int64
}

func (e Rewound) OccurredAt() time.Time { return e.At }

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

// TextDelta is text the model is writing into its message ItemID, as it
// arrives. Final means the message is the final answer, when the provider
// says so. The runner's AssistantMessage for the response follows its
// deltas and is authoritative. Only runs with Options.Stream report it
// (Capabilities.Stream).
type TextDelta struct {
	At     time.Time
	ItemID string
	Text   string
	Final  bool
}

// ReasoningDelta is text of part Part of reasoning item ItemID's summary,
// as it arrives; the runner's ReasoningSummary for the part follows.
type ReasoningDelta struct {
	At     time.Time
	ItemID string
	Part   int
	Text   string
}

// StreamReset means the text streamed since the last response is void: its
// request failed, was canceled, or started over in a new attempt.
type StreamReset struct {
	At time.Time
}

func (e TextDelta) OccurredAt() time.Time      { return e.At }
func (e ReasoningDelta) OccurredAt() time.Time { return e.At }
func (e StreamReset) OccurredAt() time.Time    { return e.At }
