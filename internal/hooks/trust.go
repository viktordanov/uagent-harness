package hooks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Why a project hook does not run (TrustState).
const (
	ReasonUntrusted     = "project hook not trusted; run `uah hooks trust` to allow it"
	ReasonScriptChanged = "the script changed; run `uah hooks trust` to allow it again"
	ReasonScriptNew     = "trusted before uah recorded its script's content; run `uah hooks trust` again"
)

// Trust records the exact project hook commands the user approved, by
// SHA-256, so a changed command needs approval again. When a command runs a
// local script (its first word is a path to an existing file), the entry
// also records the script's SHA-256, so a changed script needs approval
// again too.
//
// Entries written before script hashes existed are keyed by the command
// alone. They still approve commands that run no local script; a command
// that runs one needs `uah hooks trust` again.
type Trust struct {
	path string

	mu      sync.Mutex
	entries map[string]trustEntry
}

type trustEntry struct {
	Command   string    `json:"command"`
	Workspace string    `json:"workspace"`
	TrustedAt time.Time `json:"trusted_at"`
	// Script and ScriptSHA256 are set when the command runs a local script.
	Script       string `json:"script,omitempty"`
	ScriptSHA256 string `json:"script_sha256,omitempty"`
}

// LoadTrust reads the trust file; a missing file is empty.
func LoadTrust(path string) (*Trust, error) {
	t := &Trust{path: path, entries: map[string]trustEntry{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return t, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read hook trust: %w", err)
	}
	if err := json.Unmarshal(data, &t.entries); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}

	return t, nil
}

// Check reports whether command, run in workspace, was approved as it is
// now, and if not, why.
func (t *Trust) Check(workspace, command string) (bool, string) {
	script := scriptPath(command, workspace)
	if script == "" {
		t.mu.Lock()
		_, ok := t.entries[hash(command)]
		t.mu.Unlock()
		if !ok {
			return false, ReasonUntrusted
		}

		return true, ""
	}
	sum, err := fileHash(script)
	if err != nil {
		return false, fmt.Sprintf("cannot read the script %s: %v", script, err)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.entries[scriptKey(command, script)]
	switch {
	case ok && e.ScriptSHA256 == sum:
		return true, ""
	case ok:
		return false, ReasonScriptChanged
	}
	if _, legacy := t.entries[hash(command)]; legacy {
		return false, ReasonScriptNew
	}

	return false, ReasonUntrusted
}

// Trusted reports whether command, run in workspace, may run.
func (t *Trust) Trusted(workspace, command string) bool {
	ok, _ := t.Check(workspace, command)

	return ok
}

// Allow approves commands as they are now, with the content of any local
// script they run, and saves the file atomically.
func (t *Trust) Allow(workspace string, commands ...string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now().UTC()
	for _, c := range commands {
		e := trustEntry{Command: c, Workspace: workspace, TrustedAt: now}
		key := hash(c)
		if script := scriptPath(c, workspace); script != "" {
			sum, err := fileHash(script)
			if err != nil {
				return fmt.Errorf("failed to read the hook script: %w", err)
			}
			e.Script, e.ScriptSHA256, key = script, sum, scriptKey(c, script)
		}
		t.entries[key] = e
	}
	data, err := json.MarshalIndent(t.entries, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode hook trust: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(t.path), 0o700); err != nil {
		return fmt.Errorf("failed to create %s: %w", filepath.Dir(t.path), err)
	}
	tmp := t.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("failed to write hook trust: %w", err)
	}
	if err := os.Rename(tmp, t.path); err != nil {
		return fmt.Errorf("failed to save hook trust: %w", err)
	}

	return nil
}

func hash(command string) string {
	sum := sha256.Sum256([]byte(command))

	return hex.EncodeToString(sum[:])
}

// scriptKey keys a command that runs a script by the script's path too: the
// same relative command runs a different file in another workspace.
func scriptKey(command, script string) string { return hash(command + "\x00" + script) }

func fileHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err //nolint:wrapcheck // the callers say what failed
	}
	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:]), nil
}
