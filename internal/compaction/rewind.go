package compaction

import (
	"path/filepath"
	"time"
)

// Rewind is a cut in a session's history, as Codex's backtrack makes one:
// the runner's session items with sequences From through To left the
// model's context when the user went back to an earlier message. The
// session file keeps them; only what the context builder reads changes.
type Rewind struct {
	At time.Time `json:"at"`
	// MessageID is the message the session went back to: the first item
	// the cut drops is it, or an input that went with it.
	MessageID string `json:"message_id"`
	From      uint64 `json:"from"`
	To        uint64 `json:"to"`
	// Tokens is the context in use that the last response before the cut
	// reported (0: unknown).
	Tokens int64 `json:"tokens,omitempty"`
}

// Cuts are a session's rewinds, in the order they were made.
type Cuts []Rewind

// Hides reports whether a rewind dropped the item with this sequence.
func (c Cuts) Hides(seq uint64) bool {
	for _, r := range c {
		if seq >= r.From && seq <= r.To {
			return true
		}
	}

	return false
}

// After reports whether a rewind was made after t: a compaction made
// before it may cover what it dropped.
func (c Cuts) After(t time.Time) bool {
	return len(c) > 0 && c[len(c)-1].At.After(t)
}

// RewindLog is a session's rewinds, one JSON line each, in
// sessions/<id>.rewind.jsonl next to the runner's session file.
type RewindLog struct{ path string }

// OpenRewinds returns the rewind log of the session id in sessionsDir. It
// does not touch the file.
func OpenRewinds(sessionsDir, id string) RewindLog {
	return RewindLog{path: filepath.Join(sessionsDir, id+".rewind.jsonl")}
}

// Records reads every rewind in order, skipping lines that do not decode.
func (l RewindLog) Records() (Cuts, error) {
	cuts, _, err := readLines(l.path, "rewind", func(r Rewind) bool { return r.MessageID != "" && r.From > 0 && r.To >= r.From })

	return cuts, err
}

// Append adds a rewind and syncs it.
func (l RewindLog) Append(r Rewind) error { return appendLine(l.path, "rewind", r) }
