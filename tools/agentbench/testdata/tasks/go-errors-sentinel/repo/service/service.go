// Package service implements operations over the store.
package service

import (
	"fmt"

	"example.com/kv/store"
)

// Service wraps a store.
type Service struct{ St *store.Store }

// Rename moves a key's value to a new key, which must not exist.
func (s Service) Rename(from, to string) error {
	v, _, err := s.St.Get(from)
	if err != nil {
		return fmt.Errorf("service: rename %q: %v", from, err)
	}
	if _, err := s.St.Put(to, v, 0); err != nil {
		return fmt.Errorf("service: rename %q to %q: %v", from, to, err)
	}
	if err := s.St.Delete(from); err != nil {
		return fmt.Errorf("service: rename %q: %v", from, err)
	}

	return nil
}

// Append appends text to a key's value at the given version.
func (s Service) Append(key, text string, version int) (int, error) {
	v, _, err := s.St.Get(key)
	if err != nil {
		return 0, fmt.Errorf("service: append %q: %s", key, err.Error())
	}
	n, err := s.St.Put(key, v+text, version)
	if err != nil {
		return 0, fmt.Errorf("service: append %q: %s", key, err.Error())
	}

	return n, nil
}
