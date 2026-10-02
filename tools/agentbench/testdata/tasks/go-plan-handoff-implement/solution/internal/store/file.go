package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

// File is a Store backed by a file of JSON lines, one link per line,
// appended on each Put and read back on open, so links survive a restart.
type File struct {
	mu  sync.Mutex
	mem *Memory
	f   *os.File
}

type record struct {
	Code string `json:"code"`
	URL  string `json:"url"`
}

// OpenFile opens or creates the store at path and loads its links.
func OpenFile(path string) (*File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	mem := NewMemory()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var r record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			f.Close()

			return nil, fmt.Errorf("store: %s: %w", path, err)
		}
		mem.links[r.Code] = r.URL
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		f.Close()

		return nil, err
	}

	return &File{mem: mem, f: f}, nil
}

// Put saves url under a new code and appends it to the file.
func (s *File) Put(url string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	code, err := s.mem.Put(url)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(record{Code: code, URL: url})
	if err != nil {
		return "", err
	}
	if _, err := s.f.Write(append(b, '\n')); err != nil {
		return "", err
	}

	return code, s.f.Sync()
}

// Get returns the URL saved under code.
func (s *File) Get(code string) (string, error) { return s.mem.Get(code) }

// Close closes the file.
func (s *File) Close() error { return s.f.Close() }
