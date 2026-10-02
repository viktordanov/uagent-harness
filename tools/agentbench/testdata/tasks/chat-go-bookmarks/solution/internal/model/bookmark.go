// Package model holds the bookmark type shared by the store and the API.
package model

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// ErrInvalid is wrapped by every validation error, so callers can map it
// to a 400 with errors.Is.
var ErrInvalid = errors.New("invalid bookmark")

// MaxNameLen is the longest name a bookmark may have, in bytes.
const MaxNameLen = 200

// Bookmark is one saved link.
type Bookmark struct {
	ID        int64     `json:"id"`
	URL       string    `json:"url"`
	Name      string    `json:"name"`
	Tags      []string  `json:"tags"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Validate reports the first problem with b, wrapped in ErrInvalid.
func (b Bookmark) Validate() error {
	if strings.TrimSpace(b.URL) == "" {
		return fmt.Errorf("%w: url is required", ErrInvalid)
	}
	u, err := url.Parse(b.URL)
	if err != nil {
		return fmt.Errorf("%w: url: %v", ErrInvalid, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: url must be http or https", ErrInvalid)
	}
	if u.Host == "" {
		return fmt.Errorf("%w: url has no host", ErrInvalid)
	}
	if strings.TrimSpace(b.Name) == "" {
		return fmt.Errorf("%w: name is required", ErrInvalid)
	}
	if len(b.Name) > MaxNameLen {
		return fmt.Errorf("%w: name is longer than %d bytes", ErrInvalid, MaxNameLen)
	}
	for _, t := range b.Tags {
		if err := ValidateTag(t); err != nil {
			return err
		}
	}
	return nil
}

// Clone returns a copy of b that shares no memory with it.
func (b Bookmark) Clone() Bookmark {
	c := b
	if b.Tags != nil {
		c.Tags = append([]string(nil), b.Tags...)
	}
	return c
}

// Matches reports whether q occurs in the name or the URL, ignoring case.
// An empty q matches everything.
func (b Bookmark) Matches(q string) bool {
	if q == "" {
		return true
	}
	q = strings.ToLower(q)
	return strings.Contains(strings.ToLower(b.Name), q) ||
		strings.Contains(strings.ToLower(b.URL), q)
}
