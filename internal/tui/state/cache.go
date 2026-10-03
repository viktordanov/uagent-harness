package state

import (
	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/internal/usage/cachestats"
)

type (
	// EffLoadCache reads the prompt cache accounting of the open session
	// from its recorded runs (internal/cachestats).
	EffLoadCache struct{ SessionID string }
	// CacheLoaded is the session's cache accounting, or why it is missing.
	CacheLoaded struct {
		Summary cachestats.Summary
		Err     error
	}
)

func (EffLoadCache) effect() {}

// loadCache asks for the open session's cache accounting, for /usage and
// /status; before the session has an ID there is none to read.
func (s *State) loadCache() []Effect {
	if s.SessionID == "" {
		return nil
	}

	return []Effect{EffLoadCache{SessionID: s.SessionID}}
}

// onCache handles CacheLoaded and reports whether ev was one: a line such
// as "prompt cache 88% · missed 41k: effort switches 28k, idle 9k, cold
// start 4k · ≈3.1% of usage (API-price estimate)".
func (s *State) onCache(ev any) ([]Effect, bool) {
	e, ok := ev.(CacheLoaded)
	if !ok {
		return nil, false
	}
	if e.Err != nil {
		s.notice(LevelDebug, "cache: "+e.Err.Error())

		return nil, true
	}
	s.notice(session.LevelInfo, "prompt "+e.Summary.Line())

	return nil, true
}
