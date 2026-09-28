package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// newSessionID checks --session-id, as Claude Code's flag: the ID of a new
// session, a UUID that no session in stateDir has. It never resumes; a
// session to resume comes from in.SessionRef.
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
	dir := filepath.Join(stateDir, "sessions")
	for _, name := range []string{id + ".session.jsonl", id + ".uah.json"} {
		_, err := os.Stat(filepath.Join(dir, name))
		if err == nil {
			return "", usage(fmt.Errorf("session %s exists; resume it with uah resume %s", id, id))
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("failed to check the session ID: %w", err)
		}
	}

	return id, nil
}
