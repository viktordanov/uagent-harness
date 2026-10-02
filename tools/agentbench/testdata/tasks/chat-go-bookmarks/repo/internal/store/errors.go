package store

import "errors"

var (
	// ErrNotFound is returned when no bookmark has the given id.
	ErrNotFound = errors.New("bookmark not found")
	// ErrDuplicateURL is returned when another bookmark already has the URL.
	ErrDuplicateURL = errors.New("a bookmark with this url already exists")
)
