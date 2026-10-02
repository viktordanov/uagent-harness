package clock

import (
	"context"
	"sync"
	"time"
)

// Epoch is the time a Fake starts at when NewFake is given the zero time.
var Epoch = time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)

// Fake is a manual clock for tests. Its time only moves when Advance, Set or
// Sleep is called. Sleep moves the clock forward by the duration and returns
// at once, so a test that "sleeps" for an hour takes no time, and every
// duration passed to Sleep is recorded for the test to inspect.
//
// A Fake is safe for concurrent use.
type Fake struct {
	mu     sync.Mutex
	now    time.Time
	sleeps []time.Duration
}

// NewFake returns a fake clock that reads start, or Epoch when start is the
// zero time.
func NewFake(start time.Time) *Fake {
	if start.IsZero() {
		start = Epoch
	}
	return &Fake{now: start}
}

// Now returns the fake time.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Advance moves the clock forward by d. A negative d is ignored: the fake
// clock never runs backwards.
func (f *Fake) Advance(d time.Duration) {
	if d <= 0 {
		return
	}
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.mu.Unlock()
}

// Set moves the clock to t, if t is after the current fake time.
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	if t.After(f.now) {
		f.now = t
	}
	f.mu.Unlock()
}

// Sleep records d and advances the clock by it. It returns ctx.Err() without
// advancing when ctx is already done.
func (f *Fake) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	f.sleeps = append(f.sleeps, d)
	if d > 0 {
		f.now = f.now.Add(d)
	}
	f.mu.Unlock()
	return nil
}

// Sleeps returns a copy of the durations passed to Sleep so far, in order.
func (f *Fake) Sleeps() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Duration(nil), f.sleeps...)
}

// ClearSleeps forgets the recorded sleeps.
func (f *Fake) ClearSleeps() {
	f.mu.Lock()
	f.sleeps = nil
	f.mu.Unlock()
}
