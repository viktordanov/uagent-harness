// Package notify sends shipment events to subscribers.
package notify

import (
	"context"
	"errors"

	"example.com/shipd/internal/store"
)

// Message is one event for one subscriber.
type Message struct {
	URL    string
	Secret string
	Event  store.Event
}

// Sender delivers a message once; it never retries (the queue does).
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// PermanentError is a failure that retrying cannot fix, such as a 4xx.
type PermanentError struct{ Err error }

func (e *PermanentError) Error() string { return e.Err.Error() }
func (e *PermanentError) Unwrap() error { return e.Err }

// Retryable reports whether a send may succeed if tried again.
func Retryable(err error) bool {
	var p *PermanentError

	return !errors.As(err, &p)
}
