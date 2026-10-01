// Package model holds the domain types.
package model

import (
	"errors"
	"strings"
)

// Account is a registered user.
type Account struct {
	ID    int
	Email string
	Name  string
	Admin bool
}

// ErrInvalid is returned by NewAccount for a bad email or name.
var ErrInvalid = errors.New("invalid user")

// NewAccount validates and builds a Account.
func NewAccount(id int, email, name string) (Account, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") || strings.TrimSpace(name) == "" {
		return Account{}, ErrInvalid
	}

	return Account{ID: id, Email: email, Name: strings.TrimSpace(name)}, nil
}
