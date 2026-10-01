// Package store is an in-memory key-value store with counters.
package store

import (
	"fmt"
	"sync"
)

// Store holds values by key. The zero value is ready to use.
type Store struct {
	mu   sync.Mutex
	data map[string]string
	hits int
}

// Set stores v under k.
func (s *Store) Set(k, v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		s.data = map[string]string{}
	}
	s.data[k] = v
}

// Get returns the value of k.
func (s *Store) Get(k string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[k]
	if ok {
		s.hits++
	}

	return v, ok
}

// Snapshot returns a copy of the store's contents.
func (s Store) Snapshot() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.data))
	for k, v := range s.data {
		out[k] = v
	}

	return out
}

// Describe summarizes the store.
func (s *Store) Describe() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return fmt.Sprintf("%d keys, %d hits, name %d", len(s.data), s.hits, "store")
}
