// Package clock lets the queue and its workers read the time and wait
// without calling the time package directly, so that tests can run them on
// a fake clock instead of the wall clock.
package clock

import (
	"context"
	"time"
)

// Clock is the time source of the queue, the stores and the workers.
type Clock interface {
	// Now returns the current time.
	Now() time.Time
	// Sleep waits for d, or until ctx is done, whichever comes first. It
	// returns ctx.Err() when ctx ended the wait, and nil otherwise. A
	// duration of zero or less does not wait.
	Sleep(ctx context.Context, d time.Duration) error
}

// Real is the wall clock.
type Real struct{}

// System returns the wall clock.
func System() Clock { return Real{} }

// Now returns time.Now().
func (Real) Now() time.Time { return time.Now() }

// Sleep waits on a timer for d, or until ctx is done.
func (Real) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Since returns the time elapsed on c since t.
func Since(c Clock, t time.Time) time.Duration {
	return c.Now().Sub(t)
}

// Deadline returns the time d after now on c, or the zero time when d is zero
// or less (no deadline).
func Deadline(c Clock, d time.Duration) time.Time {
	if d <= 0 {
		return time.Time{}
	}
	return c.Now().Add(d)
}
