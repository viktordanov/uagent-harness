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

// Trust records the exact project hook commands the user approved, by
// SHA-256, so a changed command needs approval again.
type Trust struct {
	path string

	mu      sync.Mutex
	entries map[string]trustEntry
}

type trustEntry struct {
	Command   string    `json:"command"`
	Workspace string    `json:"workspace"`
	TrustedAt time.Time `json:"trusted_at"`
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

// Trusted reports whether this exact command was approved.
func (t *Trust) Trusted(command string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.entries[hash(command)]

	return ok
}

// Allow approves commands and saves the file atomically.
func (t *Trust) Allow(workspace string, commands ...string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, c := range commands {
		t.entries[hash(c)] = trustEntry{Command: c, Workspace: workspace, TrustedAt: time.Now().UTC()}
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
