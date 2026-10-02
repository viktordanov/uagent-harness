// Package retry decides whether a job whose attempt failed gets another
// attempt, and how long it waits before it.
package retry

import (
	"fmt"
	"time"
)

// The defaults a Policy falls back to, and what jobq run uses.
const (
	// DefaultMaxAttempts is how many times a job is attempted in all,
	// counting the first attempt.
	DefaultMaxAttempts = 3
	// DefaultDelay is the wait before each retry.
	DefaultDelay = 200 * time.Millisecond
)

// Policy is a retry policy: a job is attempted up to MaxAttempts times, and
// waits Delay before each retry.
type Policy struct {
	// MaxAttempts caps the attempts of a job, the first one included. Zero
	// or less means DefaultMaxAttempts.
	MaxAttempts int
	// Delay is the wait before every retry. Zero or less retries at once.
	Delay time.Duration
}

// Default returns the policy jobq uses unless told otherwise.
func Default() Policy {
	return Policy{MaxAttempts: DefaultMaxAttempts, Delay: DefaultDelay}
}

// Attempts returns the effective cap on attempts.
func (p Policy) Attempts() int {
	if p.MaxAttempts <= 0 {
		return DefaultMaxAttempts
	}
	return p.MaxAttempts
}

// Next is called after attempt number attempt (1 for the first) of a job
// failed. It returns the wait before the next attempt and true, or false when
// the job has used all its attempts.
func (p Policy) Next(attempt int) (time.Duration, bool) {
	if attempt >= p.Attempts() {
		return 0, false
	}
	return p.delay(), true
}

// Schedule returns the waits a job would see if every attempt failed: one per
// retry, so Attempts()-1 of them.
func (p Policy) Schedule() []time.Duration {
	var out []time.Duration
	for attempt := 1; ; attempt++ {
		d, ok := p.Next(attempt)
		if !ok {
			return out
		}
		out = append(out, d)
	}
}

// Validate reports a policy that makes no sense.
func (p Policy) Validate() error {
	if p.MaxAttempts < 0 {
		return fmt.Errorf("retry: negative max attempts %d", p.MaxAttempts)
	}
	if p.Delay < 0 {
		return fmt.Errorf("retry: negative delay %s", p.Delay)
	}
	return nil
}

// String describes the policy for logs.
func (p Policy) String() string {
	return fmt.Sprintf("up to %d attempts, %s apart", p.Attempts(), p.delay())
}

func (p Policy) delay() time.Duration {
	if p.Delay < 0 {
		return 0
	}
	return p.Delay
}
