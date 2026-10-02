// Package store keeps bookmarks in memory.
package store

import (
	"sort"
	"strings"
	"sync"
	"time"

	"example.com/bookmarks/internal/model"
)

// Store is an in-memory bookmark store. IDs start at 1 and are never reused.
// It is safe for concurrent use.
type Store struct {
	mu     sync.RWMutex
	items  map[int64]model.Bookmark
	nextID int64
	now    func() time.Time
}

// New returns an empty store.
func New() *Store {
	return &Store{
		items:  make(map[int64]model.Bookmark),
		nextID: 1,
		now:    func() time.Time { return time.Now().UTC() },
	}
}

// SetClock replaces the clock used for CreatedAt and UpdatedAt. For tests.
func (s *Store) SetClock(now func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = now
}

// Create validates b, gives it an id and timestamps, and stores it.
func (s *Store) Create(b model.Bookmark) (model.Bookmark, error) {
	b.Tags = model.NormalizeTags(b.Tags)
	if err := b.Validate(); err != nil {
		return model.Bookmark{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.urlTaken(b.URL, 0) {
		return model.Bookmark{}, ErrDuplicateURL
	}
	b.ID = s.nextID
	s.nextID++
	b.CreatedAt = s.now()
	b.UpdatedAt = b.CreatedAt
	s.items[b.ID] = b.Clone()
	return b, nil
}

// Get returns the bookmark with the given id.
func (s *Store) Get(id int64) (model.Bookmark, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.items[id]
	if !ok {
		return model.Bookmark{}, ErrNotFound
	}
	return b.Clone(), nil
}

// Update replaces the URL, title and tags of the bookmark with the given id.
func (s *Store) Update(id int64, b model.Bookmark) (model.Bookmark, error) {
	b.Tags = model.NormalizeTags(b.Tags)
	if err := b.Validate(); err != nil {
		return model.Bookmark{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.items[id]
	if !ok {
		return model.Bookmark{}, ErrNotFound
	}
	if s.urlTaken(b.URL, id) {
		return model.Bookmark{}, ErrDuplicateURL
	}
	b.ID = id
	b.CreatedAt = old.CreatedAt
	b.UpdatedAt = s.now()
	s.items[id] = b.Clone()
	return b, nil
}

// Delete removes the bookmark with the given id.
func (s *Store) Delete(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.items[id]; !ok {
		return ErrNotFound
	}
	delete(s.items, id)
	return nil
}

// List returns every bookmark, ordered by id.
func (s *Store) List() []model.Bookmark {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Bookmark, 0, len(s.items))
	for _, b := range s.items {
		out = append(out, b.Clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Len returns the number of bookmarks.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.items)
}

// urlTaken reports whether a bookmark other than except has the URL.
// The caller holds s.mu.
// URLs are compared without a trailing slash and ignoring case.
func (s *Store) urlTaken(u string, except int64) bool {
	key := urlKey(u)
	for id, b := range s.items {
		if id != except && urlKey(b.URL) == key {
			return true
		}
	}
	return false
}

func urlKey(u string) string {
	return strings.ToLower(strings.TrimSuffix(u, "/"))
}
