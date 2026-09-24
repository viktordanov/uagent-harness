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
	window := compaction.ContextWindow(s.Settings.Model, s.Settings.ContextWindow, s.Windows)

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
		if e.Trigger == compaction.TriggerClear {
			return true // /clear already said so
		}
		text := "Compacting the context"
		if e.Trigger == compaction.TriggerAuto {
			text += " (automatic: the context is nearly full)"
		}
		s.notice(session.LevelInfo, text)
	case engine.Compacted:
		if e.Interrupted {
			s.notice(session.LevelInfo, "Compaction interrupted")

			return true
		}
		if e.Err != "" {
			s.notice(session.LevelWarning, e.Trigger.Verb()+" failed: "+e.Err)

			return true
		}
		s.ContextUsed = 0
		if e.Trigger == compaction.TriggerClear {
			s.notice(LevelDebug, "context cleared")

			return true
		}
		s.notice(session.LevelInfo, "Context compacted; your messages stay as written")
		if e.Warning != "" {
			s.notice(session.LevelWarning, e.Warning)
		}
		s.notice(LevelDebug, "summary: "+e.Summary)
	case engine.AutoReviewed:
		s.onAutoReviewed(e)
	case engine.AgentUpdated:
		s.onAgentUpdated(e)
	case engine.AgentActivity:
		s.onAgentActivity(e)
	case engine.PatchApplied:
		s.onPatchApplied(e)
	default:
		return false
	}

	return true
}

// cmdClear drops the context and stays in the same session: the screen
// clears, and the agent's next request starts fresh. /new starts a new
// session instead.
func cmdClear(s *State, _ string) []Effect {
	if !s.Caps.Compaction {
		s.notice(session.LevelWarning, "/clear needs the embedded engine; /new starts a new session")

		return nil
	}
	s.Items, s.index, s.agentIDs, s.Scroll, s.ContextUsed = nil, map[string]int{}, nil, 0, 0
	s.notice(session.LevelInfo, "Context cleared: the agent starts fresh in this session. The session keeps its history; /new starts a new session")

	return []Effect{EffClear{}}
}

// cmdCompact compacts; its argument is what the summary should focus on,
// as Claude Code's /compact [instructions].
func cmdCompact(s *State, args string) []Effect {
	if !s.Caps.Compaction {
		s.notice(session.LevelWarning, "/compact needs the embedded engine")

		return nil
	}

	return []Effect{EffCompact{Focus: args}}
}
