// Package model holds the domain types.
package model

import (
	"errors"
	"strings"
)

// UserRecord is a registered user.
type UserRecord struct {
	ID    int
	Email string
	Name  string
	Admin bool
}

// ErrInvalid is returned by NewUserRecord for a bad email or name.
var ErrInvalid = errors.New("invalid user")

// NewUserRecord validates and builds a UserRecord.
func NewUserRecord(id int, email, name string) (UserRecord, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") || strings.TrimSpace(name) == "" {
		return UserRecord{}, ErrInvalid
	}

	return UserRecord{ID: id, Email: email, Name: strings.TrimSpace(name)}, nil
}
