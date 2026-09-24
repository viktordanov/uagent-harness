package session

import (
	"errors"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
)

// AgentWatch follows one subagent of a session, for a view of its
// transcript: its runs from before this process, the events its session
// reported since it opened, and the ones that follow.
type AgentWatch struct {
	ID, Nickname string
	History      []LoadedRun
	Events       []core.Event
	// Next brings the events that follow. It closes when the agent's
	// session closes, when Stop is called, or when the view fell too far
	// behind (open it again to catch up).
	Next <-chan core.Event
	Stop func()
	// Send gives the agent a message, as the parent's send_input does; now
	// steers it into the agent's live run, as ctrl+enter does.
	Send func(text string, now bool) error
	// Interrupt stops the agent's current work, and its own subagents',
	// as esc esc does for the main agent. It stays open for more messages.
	Interrupt func()
}

// AgentWatcher follows a session's subagents; internal/agents implements
// it.
type AgentWatcher interface {
	WatchAgent(parentID, id string) (*AgentWatch, error)
}

// SubagentsEngine is an engine that runs subagents.
type SubagentsEngine interface {
	Subagents() engine.Subagents
}

// ErrNoSubagents means the session's engine runs no subagents.
var ErrNoSubagents = errors.New("this session has no subagents")

// WatchAgent follows one of the session's subagents, by ID.
func (s *Session) WatchAgent(id string) (*AgentWatch, error) {
	e, ok := s.eng.(SubagentsEngine)
	if !ok {
		return nil, ErrNoSubagents
	}
	w, ok := e.Subagents().(AgentWatcher)
	if !ok {
		return nil, ErrNoSubagents
	}

	return w.WatchAgent(s.id, id) //nolint:wrapcheck // the agents' own errors
}
