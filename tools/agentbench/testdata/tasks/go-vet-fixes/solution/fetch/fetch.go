// Package fetch runs work with a deadline.
package fetch

import (
	"context"
	"errors"
	"time"
)

// ErrSlow is returned when the work misses the deadline.
var ErrSlow = errors.New("too slow")

// WithDeadline runs work and gives up after d.
func WithDeadline(parent context.Context, d time.Duration, work func(context.Context) (string, error)) (string, error) {
	ctx, cancel := context.WithTimeout(parent, d)
	defer cancel()
	type result struct {
		v   string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		v, err := work(ctx)
		ch <- result{v, err}
	}()
	select {
	case r := <-ch:
		return r.v, r.err
	case <-ctx.Done():
		return "", ErrSlow
	}
}
