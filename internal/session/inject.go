package session

import (
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
// wait_agent.
func (s *Session) Inject(text string) { s.post(evDo(func() { s.onInject(text) })) }

func (s *Session) onInject(text string) {
	if s.state == StateClosed {
		return
	}
	s.held = append(s.held, core.UserInput{ID: uuid.NewString(), Text: text})
}
