// Package migrate moves uah's files, once, from the folders it used before
// ~/.uah: the configuration directory (~/.config/uagent) and uah's part of
// the state directory it shared with uagent (~/.local/state/unreal-agent).
// It copies and never deletes, so the old folders stay as they were.
package migrate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/viktordanov/uagent-harness/internal/home"
	"github.com/viktordanov/uagent-harness/internal/store"
)

// Marker is the file in the home that records a migration.
const Marker = "migrated.json"

// Done is the line printed after a migration.
const Done = "moved your config and sessions to ~/.uah (the old folders are untouched)"

// stateEntries are what uah copies from the old state directory: sessions
// (with their sidecars and compaction logs), run records, pasted images, and
// the model cache. The index goes through SQLite; logs and the sandbox's
// scratch files stay behind.
var stateEntries = []string{"sessions", "runs", "images", "models"}

// Paths are the new home and the old folders.
type Paths struct {
	Home string
	// Config is the old configuration directory.
	Config string
	// State is the old state directory.
	State string
}

// Record is the marker's content.
type Record struct {
	Config string    `json:"config,omitempty"`
	State  string    `json:"state,omitempty"`
	At     time.Time `json:"at"`
}

// Old returns the old folders for this environment: $XDG_CONFIG_HOME/uagent
// or ~/.config/uagent, and $UAGENT_STATE_DIR, $XDG_STATE_HOME/unreal-agent,
// or ~/.local/state/unreal-agent. The home is ~/.uah.
func Old(getenv func(string) string, userHome string) Paths {
	p := Paths{
		Home:   filepath.Join(userHome, home.Name),
		Config: filepath.Join(userHome, ".config", "uagent"),
		State:  filepath.Join(userHome, ".local", "state", "unreal-agent"),
	}
	if dir := getenv("XDG_CONFIG_HOME"); dir != "" {
		p.Config = filepath.Join(dir, "uagent")
	}
	if dir := getenv("XDG_STATE_HOME"); dir != "" {
		p.State = filepath.Join(dir, "unreal-agent")
	}
	if dir := getenv("UAGENT_STATE_DIR"); dir != "" {
		p.State = dir
	}

	return p
}

// Pending reports whether p should migrate: the home does not exist yet and
// an old folder has something to copy.
func (p Paths) Pending() bool { return !exists(p.Home) && p.HasOld() }

// HasOld reports whether the old configuration directory exists or the old
// state directory holds something uah copies.
func (p Paths) HasOld() bool {
	if isDir(p.Config) {
		return true
	}
	for _, name := range append([]string{"uah.db"}, stateEntries...) {
		if exists(filepath.Join(p.State, name)) {
			return true
		}
	}

	return false
}

// Auto migrates at startup when $UAH_HOME is unset and the migration is
// pending. It reports whether it migrated; on an error, ~/.uah does not
// exist and the old folders are as they were.
func Auto(ctx context.Context, getenv func(string) string) (bool, error) {
	if getenv(home.Env) != "" {
		return false, nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return false, nil //nolint:nilerr // no home directory, nothing to migrate
	}
	p := Old(getenv, userHome)
	if !p.Pending() {
		return false, nil
	}

	return Run(ctx, p)
}

// Run copies the old folders into a temporary directory next to the home and
// renames it to the home, so the home appears whole or not at all. It
// reports false with no error when another process created the home first.
func Run(ctx context.Context, p Paths) (bool, error) {
	parent := filepath.Dir(p.Home)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return false, fmt.Errorf("failed to create %s: %w", parent, err)
	}
	tmp, err := os.MkdirTemp(parent, filepath.Base(p.Home)+".migrating-*")
	if err != nil {
		return false, fmt.Errorf("failed to create a temporary home: %w", err)
	}
	defer os.RemoveAll(tmp) // gone after the rename; the partial copy on failure
	if err := fill(ctx, p, tmp); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, p.Home); err != nil {
		if exists(p.Home) {
			return false, nil
		}

		return false, fmt.Errorf("failed to move the new home into place: %w", err)
	}

	return true, nil
}

// fill copies the configuration, then the state, and writes the marker.
func fill(ctx context.Context, p Paths, dst string) error {
	rec := Record{At: time.Now().UTC()}
	if isDir(p.Config) {
		if err := copyTree(p.Config, dst); err != nil {
			return fmt.Errorf("failed to copy %s: %w", p.Config, err)
		}
		rec.Config = p.Config
	}
	for _, name := range stateEntries {
		src := filepath.Join(p.State, name)
		if !isDir(src) {
			continue
		}
		if err := copyTree(src, filepath.Join(dst, name)); err != nil {
			return fmt.Errorf("failed to copy %s: %w", src, err)
		}
		rec.State = p.State
	}
	if err := store.CopyIndex(ctx, p.State, dst, p.Home); err != nil {
		// The index is rebuilt from the run records on the next open.
		for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
			_ = os.Remove(filepath.Join(dst, "uah.db"+suffix))
		}
	} else if exists(filepath.Join(dst, "uah.db")) {
		rec.State = p.State
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode the migration record: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dst, Marker), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("failed to write the migration record: %w", err)
	}

	return nil
}

// ReadRecord reads the marker in dir; found is false when there is none.
func ReadRecord(dir string) (rec Record, found bool, err error) {
	data, err := os.ReadFile(filepath.Join(dir, Marker))
	if errors.Is(err, fs.ErrNotExist) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, fmt.Errorf("failed to read the migration record: %w", err)
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		return Record{}, false, fmt.Errorf("failed to decode the migration record: %w", err)
	}

	return rec, true, nil
}

func exists(path string) bool {
	_, err := os.Lstat(path)

	return err == nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.IsDir()
}
