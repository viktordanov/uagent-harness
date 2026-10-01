// Package service is the users' business logic.
package service

import (
	"errors"

	"example.com/accounts/model"
	"example.com/accounts/store"
)

// ErrExists is returned when registering an email twice.
var ErrExists = errors.New("email already registered")

// Service registers and lists users.
type Service struct {
	st   *store.Store
	next int
}

// New returns a service over st.
func New(st *store.Store) *Service { return &Service{st: st, next: 1} }

// Register creates a UserRecord with the next ID.
func (s *Service) Register(email, name string) (model.UserRecord, error) {
	u, err := model.NewUserRecord(s.next, email, name)
	if err != nil {
		return model.UserRecord{}, err
	}
	for _, other := range s.st.List() {
		if other.Email == u.Email {
			return model.UserRecord{}, ErrExists
		}
	}
	s.next++
	s.st.Put(u)

	return u, nil
}
