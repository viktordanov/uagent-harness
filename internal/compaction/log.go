package compaction

import (
	"bufio"
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Log is a session's compactions, one JSON line each, in
// sessions/<id>.compaction.jsonl next to the runner's session file. The last
// readable line applies.
type Log struct{ path string }

// OpenLog returns the log of the session id in sessionsDir. It does not
// touch the file.
func OpenLog(sessionsDir, id string) Log {
	return Log{path: filepath.Join(sessionsDir, id+".compaction.jsonl")}
}

// Path is the log's file.
func (l Log) Path() string { return l.path }

// Records reads every compaction in order. A line that does not decode (a
// write cut short by a crash, an edit) is skipped and counted in corrupt,
// so one bad line does not stop the session from resuming.
func (l Log) Records() (records []Record, corrupt int, err error) {
	return readLines(l.path, "compaction", func(rec Record) bool { return rec.Covered > 0 && rec.Hash != "" })
}

// readLines reads the JSON-lines file at path, skipping and counting the
// lines that do not decode or that valid rejects. A missing file is empty.
func readLines[T any](path, what string, valid func(T) bool) (out []T, corrupt int, err error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("failed to open the %s log: %w", what, err)
	}
	defer f.Close()
	r := bufio.NewReader(f)
	for {
		line, rerr := r.ReadBytes('\n')
		if line = bytes.TrimSpace(line); len(line) > 0 {
			var v T
			if json.Unmarshal(line, &v) == nil && valid(v) {
				out = append(out, v)
			} else {
				corrupt++
			}
		}
		if errors.Is(rerr, io.EOF) {
			return out, corrupt, nil
		}
		if rerr != nil {
			return nil, 0, fmt.Errorf("failed to read the %s log: %w", what, rerr)
		}
	}
}

// Last is the compaction that applies, or nil.
func (l Log) Last() (rec *Record, corrupt int, err error) {
	records, corrupt, err := l.Records()
	if err != nil || len(records) == 0 {
		return nil, corrupt, err
	}

	return &records[len(records)-1], corrupt, nil
}

// Append adds a compaction and syncs it. A line left without its newline by
// an earlier crash is ended first, so the new line stays readable.
func (l Log) Append(rec Record) error {
	return appendLine(l.path, "compaction", rec)
}

// appendLine adds v to the JSON-lines file at path and syncs it; what names
// the file in errors.
func appendLine(path, what string, v any) error {
	line, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to encode the %s: %w", what, err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("failed to open the %s log: %w", what, err)
	}
	if unterminated(f) {
		line = append([]byte{'\n'}, line...)
	}
	_, err = f.Write(append(line, '\n'))
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("failed to write the %s log: %w", what, err)
	}

	return nil
}

// unterminated reports whether the file is not empty and its last byte is
// not a newline.
func unterminated(f *os.File) bool {
	info, err := f.Stat()
	if err != nil || info.Size() == 0 {
		return false
	}
	last := make([]byte, 1)
	if _, err := f.ReadAt(last, info.Size()-1); err != nil {
		return false
	}

	return last[0] != '\n'
}
