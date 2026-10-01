package store

import "example.com/accounts/model"

// List returns every UserRecord sorted by name.
func (s *Store) List() []model.UserRecord {
	s.mu.Lock()
	out := make([]model.UserRecord, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, u)
	}
	s.mu.Unlock()
	model.SortByName(out)

	return out
}
