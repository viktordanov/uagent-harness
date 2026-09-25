package session

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/compaction"
	"github.com/viktordanov/uagent-harness/internal/engine"
)

var (
	// ErrNoRewind means the engine cannot go back to an earlier message.
	ErrNoRewind = errors.New("going back to an earlier message needs the embedded engine")
	// ErrRewindBusy means the agent works or a message waits.
	ErrRewindBusy = errors.New("going back to an earlier message waits until the agent is idle and nothing is queued")
)

type cmdRewind struct{ id string }

// Rewind goes back to before the message id, as Codex's backtrack: that
// message and everything after it leave the agent's context, and the next
// message continues from there. The session file keeps them, and the cut is
// saved next to it, so a resumed session keeps it. It needs an idle session
// with nothing queued; the engine reports the cut as engine.Rewound.
func (s *Session) Rewind(id string) error {
	_, err := call[struct{}](s, cmdRewind{id: id})

	return err
}

func (s *Session) onRewind(id string) error {
	r, ok := s.eng.(engine.Rewinder)
	if !ok || !s.caps.Rewind {
		return ErrNoRewind
	}
	if s.state != StateIdle || len(s.queue) > 0 || len(s.hooks.checking) > 0 {
		return ErrRewindBusy
	}
	ev, held, err := r.Rewind(s.ctx, s.id, id)
	if err != nil {
		return fmt.Errorf("failed to go back: %w", err)
	}
	// What went to the agent with the message goes again with the next one,
	// before anything held since.
	again := make([]core.UserInput, 0, len(held)+len(s.held))
	for _, text := range held {
		again = append(again, core.UserInput{ID: uuid.NewString(), Text: text})
	}
	s.held = append(again, s.held...)
	s.emit(ev)

	return nil
}

// withRewinds adds the session's saved rewinds to its loaded runs as
// engine.Rewound events, each in the run before it, so a reloaded
// transcript ends where the session went back to.
func withRewinds(stateDir, id string, runs []LoadedRun) ([]LoadedRun, error) {
	if len(runs) == 0 {
		return runs, nil
	}
	cuts, err := compaction.OpenRewinds(filepath.Join(stateDir, "sessions"), id).Records()
	if err != nil {
		return nil, err
	}
	for _, c := range cuts {
		place(runs, engine.Rewound{At: c.At, MessageID: c.MessageID, Tokens: c.Tokens})
	}

	return runs, nil
}
