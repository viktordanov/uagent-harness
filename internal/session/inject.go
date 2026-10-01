package session

import (
	"slices"

	"github.com/google/uuid"

	"github.com/viktordanov/uagent/core"
)

// evDo is work for the loop goroutine from another goroutine.
type evDo func()

// Inject gives the agent a message without a turn of its own, as Codex's
// inject_no_new_turn does for a subagent's notification: it is held and
// goes out before the next run's messages, and it never starts a run.
//
// It is not sent into a live run: the runner cancels its model request
// when a message arrives, so a notification would throw away a paid
// request. A parent that needs a child's status at once waits for it with
// wait_agent, and withdraw takes the message back while it is held.
func (s *Session) Inject(text string) (withdraw func()) {
	id := uuid.NewString()
	s.post(evDo(func() { s.onInject(id, text) }))

	return func() {
		s.post(evDo(func() { s.held = slices.DeleteFunc(s.held, func(in core.UserInput) bool { return in.ID == id }) }))
	}
}

func (s *Session) onInject(id, text string) {
	if s.state == StateClosed {
		return
	}
	s.held = append(s.held, core.UserInput{ID: id, Text: text})
}
