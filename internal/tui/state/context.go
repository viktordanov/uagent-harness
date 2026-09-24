package state

import (
	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/compaction"
	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/session"
)

// ContextLeft is Codex's "N% context left": the last response's tokens
// against the model's window. ok is false before the first response and
// right after a compaction.
func (s State) ContextLeft() (percent int, ok bool) {
	if s.ContextUsed <= 0 {
		return 0, false
	}
	window := compaction.ContextWindow(s.Settings.Model, s.Settings.ContextWindow)

	return compaction.PercentLeft(s.ContextUsed, window), true
}

// noteUsage records the context a response used.
func (s *State) noteUsage(e core.ModelResponded) {
	if used := e.Usage.InputTokens + e.Usage.OutputTokens; e.Failure == "" && used > 0 {
		s.ContextUsed = used
	}
}

// onEngineEvent handles the engine's own events and reports whether ev was one.
func (s *State) onEngineEvent(ev core.Event) bool {
	switch e := ev.(type) {
	case engine.CompactionStarted:
		text := "Compacting the context"
		if e.Trigger == compaction.TriggerAuto {
			text += " (automatic: the context is nearly full)"
		}
		s.notice(session.LevelInfo, text)
	case engine.Compacted:
		if e.Err != "" {
			s.notice(session.LevelWarning, "compaction failed: "+e.Err)

			return true
		}
		s.ContextUsed = 0
		s.notice(session.LevelInfo, "Context compacted; your messages stay as written")
		s.notice(LevelDebug, "summary: "+e.Summary)
	case engine.AutoReviewed:
		s.onAutoReviewed(e)
	default:
		return false
	}

	return true
}

func cmdCompact(s *State, _ string) []Effect {
	if !s.Caps.Compaction {
		s.notice(session.LevelWarning, "/compact needs the embedded engine")

		return nil
	}

	return []Effect{EffCompact{}}
}
