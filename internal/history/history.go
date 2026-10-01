// Package history is the prompt history file, <home>/history.jsonl, after
// Codex's message history (codex-rs/message-history, rust-v0.159.1): one
// JSON object per line, {"session_id":"…","ts":<unix seconds>,"text":"…"},
// plus uah's "workspace", the session's folder. Lines are appended under an
// exclusive lock with one write per line, private to the user (0600), and
// trimmed to a size cap by dropping the oldest lines. The TUI reads it once
// at startup and shows ↑ and ctrl+r the current workspace's prompts only
// (docs/design/prompt-history.md).
package history

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// FileName is the history file's name in uah's home, as Codex's
// ~/.codex/history.jsonl.
const FileName = "history.jsonl"

// DefaultMaxBytes caps the file when [history] max_bytes is unset. Codex
// has no default cap; uah reads the whole file at startup, so it keeps one.
const DefaultMaxBytes = 8 << 20

// Persistence says whether prompts are written, with Codex's values.
type Persistence string

const (
	// SaveAll writes every prompt (the default).
	SaveAll Persistence = "save-all"
	// None writes nothing; the file is still read, as in Codex.
	None Persistence = "none"
)

// Codex's lock retries: ten tries 100 ms apart, then the write fails.
const (
	lockTries = 10
	lockWait  = 100 * time.Millisecond
	// softCap is the share of the cap a trim leaves, so the next append
	// does not trim again (Codex's HISTORY_SOFT_CAP_RATIO).
	softCap = 0.8
)

// appending orders this process's appends, so the file lock and its ten
// tries only arbitrate between processes.
var appending sync.Mutex

// Entry is one prompt.
type Entry struct {
	SessionID string `json:"session_id"`
	TS        int64  `json:"ts"`
	Text      string `json:"text"`
	// Workspace is the session's workspace, absolute and clean; Codex's
	// lines have none. A line without it belongs to no folder.
	Workspace string `json:"workspace,omitempty"`
}

// File is the history file and its settings.
type File struct {
	Path string
	// MaxBytes caps the file; 0 or less means no cap, as in Codex.
	MaxBytes    int64
	Persistence Persistence
}

// New returns the file in dir with [history]'s keys: persistence ("" is
// save-all) and max_bytes (nil is DefaultMaxBytes).
func New(dir, persistence string, maxBytes *int64) (File, error) {
	p := Persistence(persistence)
	switch p {
	case "":
		p = SaveAll
	case SaveAll, None:
	default:
		return File{}, fmt.Errorf("invalid [history] persistence %q (want %q or %q)", persistence, SaveAll, None)
	}
	limit := int64(DefaultMaxBytes)
	if maxBytes != nil {
		limit = *maxBytes
	}

	return File{Path: filepath.Join(dir, FileName), MaxBytes: limit, Persistence: p}, nil
}

// Append adds entries in order, each line in one write under an exclusive
// lock, then trims the file to the cap. With persistence none it writes
// nothing.
func (f File) Append(entries ...Entry) error {
	if f.Persistence == None || len(entries) == 0 {
		return nil
	}
	var buf bytes.Buffer
	for _, e := range entries {
		line, err := json.Marshal(e)
		if err != nil {
			return fmt.Errorf("failed to encode a history entry: %w", err)
		}
		buf.Write(append(line, '\n'))
	}
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return fmt.Errorf("failed to create the history directory: %w", err)
	}
	file, err := os.OpenFile(f.Path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("failed to open the history file: %w", err)
	}
	defer file.Close()
	if err := private(file); err != nil {
		return err
	}
	appending.Lock()
	defer appending.Unlock()
	unlock, err := lock(file, syscall.LOCK_EX)
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := file.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("failed to append to the history file: %w", err)
	}

	return trim(file, f.MaxBytes)
}

// Load reads every entry, oldest first, under a shared lock. A missing
// file is empty; a line that is not an entry is skipped.
func (f File) Load() ([]Entry, error) {
	file, err := os.Open(f.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to open the history file: %w", err)
	}
	defer file.Close()
	unlock, err := lock(file, syscall.LOCK_SH)
	if err != nil {
		return nil, err
	}
	defer unlock()
	var out []Entry
	r := bufio.NewReader(file)
	for {
		line, err := r.ReadBytes('\n')
		var e Entry
		if len(bytes.TrimSpace(line)) > 0 && json.Unmarshal(line, &e) == nil {
			out = append(out, e)
		}
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return out, fmt.Errorf("failed to read the history file: %w", err)
		}
	}
}

// private makes the file 0600 when it is not, as Codex does on each append.
func private(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat the history file: %w", err)
	}
	if info.Mode().Perm() == 0o600 {
		return nil
	}
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("failed to make the history file private: %w", err)
	}

	return nil
}

// lock takes an advisory lock, trying ten times 100 ms apart as Codex
// does, so a stuck process cannot hang the caller.
func lock(file *os.File, how int) (func(), error) {
	fd := int(file.Fd()) //nolint:gosec // a descriptor fits an int
	for try := 0; ; try++ {
		err := syscall.Flock(fd, how|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = syscall.Flock(fd, syscall.LOCK_UN) }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) || try == lockTries-1 {
			return nil, fmt.Errorf("failed to lock the history file: %w", err)
		}
		time.Sleep(lockWait)
	}
}

// trim drops the oldest lines once the file passes maxBytes, down to 80%
// of it, keeping the newest line whatever its size, and rewrites the rest
// in place under the lock the caller holds (Codex's enforce_history_limit).
func trim(file *os.File, maxBytes int64) error {
	if maxBytes <= 0 {
		return nil
	}
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat the history file: %w", err)
	}
	if info.Size() <= maxBytes {
		return nil
	}
	data := make([]byte, info.Size())
	if _, err := file.ReadAt(data, 0); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("failed to read the history file: %w", err)
	}
	target := max(int64(float64(maxBytes)*softCap), 1)
	drop := 0
	for int64(len(data)-drop) > target {
		next := bytes.IndexByte(data[drop:], '\n')
		if next < 0 || drop+next+1 >= len(data) {
			break // the newest line stays
		}
		drop += next + 1
	}
	if drop == 0 {
		return nil
	}
	if err := file.Truncate(0); err != nil {
		return fmt.Errorf("failed to trim the history file: %w", err)
	}
	if _, err := file.Write(data[drop:]); err != nil { // O_APPEND writes at the new end, 0
		return fmt.Errorf("failed to trim the history file: %w", err)
	}

	return nil
}
