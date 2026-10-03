package session

import "path/filepath"

// TempDir is the session id's private temporary directory, $TMPDIR for its
// commands (sandbox.Policy.TempDir): tmp in its operation directory,
// sessions/operations/<id>/, so removing the session removes it too.
// Operation directories inside are named by operation ID, never "tmp".
func TempDir(sessionsDir, id string) string {
	return filepath.Join(sessionsDir, "operations", id, "tmp")
}
