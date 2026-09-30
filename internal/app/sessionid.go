package app

import (
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/viktordanov/uah/internal/session"
)

// newSessionID checks --session-id, as Claude Code's flag: the ID of a new
// session, a UUID that no session in stateDir has used. It never resumes; a
// session to resume comes from in.SessionRef. An ID whose session never ran
// (a sidecar, and no run or session file item, such as a launch stopped
// before its first message) is taken again, since nothing under it is lost.
func newSessionID(stateDir string, in Inputs) (string, error) {
	id := in.NewSessionID
	if id == "" {
		return "", nil
	}
	if in.SessionRef != "" {
		return "", usage(errors.New("--session-id starts a new session; resume one with --session or uah resume, not both"))
	}
	if _, err := uuid.Parse(id); err != nil || len(id) != len(uuid.Nil.String()) {
		return "", usage(fmt.Errorf("invalid --session-id %q (want a UUID such as %s)", id, uuid.Nil))
	}
	used, err := session.Used(stateDir, id)
	if err != nil {
		return "", fmt.Errorf("failed to check the session ID: %w", err)
	}
	if used {
		return "", usage(fmt.Errorf("session %s exists; resume it with uah resume %s", id, id))
	}

	return id, nil
}
