package state

import (
	"github.com/viktordanov/uagent-harness/internal/contextusage"
	"github.com/viktordanov/uagent-harness/internal/session"
)

type (
	// EffContext asks the session for the context breakdown.
	EffContext struct{}
	// ContextShown carries the breakdown; OK is false when there is none.
	ContextShown struct {
		Usage contextusage.Usage
		OK    bool
	}
)

func (EffContext) effect() {}

func cmdContext(*State, string) []Effect { return []Effect{EffContext{}} }

// onContextView handles ContextShown; ok is false for any other event.
func (s *State) onContextView(ev any) bool {
	e, ok := ev.(ContextShown)
	if !ok {
		return false
	}
	if !e.OK {
		s.notice(session.LevelInfo, "no context to show yet: /context breaks down the last model request on the embedded engine")

		return true
	}
	u := e.Usage
	s.put(Item{Kind: KindContext, Key: s.nextKey("context"), Context: &u})

	return true
}
