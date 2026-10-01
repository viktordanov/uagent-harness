// Package store is a versioned in-memory key-value store.
package store

import (
	"errors"
	"fmt"
	"sync"
)

// The errors callers may test for with errors.Is.
var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("version conflict")
)

type entry struct {
	value   string
	version int
}

// Store maps keys to versioned values.
type Store struct {
	mu   sync.Mutex
	data map[string]entry
}

// New returns an empty store.
func New() *Store { return &Store{data: map[string]entry{}} }

// Get returns the key's value and version.
func (s *Store) Get(key string) (string, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.data[key]
	if !ok {
		return "", 0, fmt.Errorf("store: get %q: %w", key, ErrNotFound)
	}

	return e.value, e.version, nil
}

// Put writes the key if version is its current version (0 for a new key)
// and returns the new version.
func (s *Store) Put(key, value string, version int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data[key].version != version {
		return 0, fmt.Errorf("store: put %q (have version %d, got %d): %w", key, s.data[key].version, version, ErrConflict)
	}
	s.data[key] = entry{value, version + 1}

	return version + 1, nil
}

// Delete removes the key.
func (s *Store) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[key]; !ok {
		return fmt.Errorf("store: delete %q: %w", key, ErrNotFound)
	}
	delete(s.data, key)

	return nil
}
