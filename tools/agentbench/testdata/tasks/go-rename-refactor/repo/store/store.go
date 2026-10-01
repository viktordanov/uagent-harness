// Package store keeps users in memory.
package store

import (
	"sync"

	"example.com/accounts/model"
)

// Store holds UserRecord values by ID.
type Store struct {
	mu    sync.Mutex
	users map[int]model.UserRecord
}

// New returns an empty store.
func New() *Store { return &Store{users: map[int]model.UserRecord{}} }

// Put saves u, replacing any UserRecord with the same ID.
func (s *Store) Put(u model.UserRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[u.ID] = u
}

// Get returns the UserRecord with the ID.
func (s *Store) Get(id int) (model.UserRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]

	return u, ok
}
