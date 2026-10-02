// Package retry decides whether a job whose attempt failed gets another
// attempt, and how long it waits before it.
package retry

import (
	"fmt"
	"math/rand/v2"
	"time"
)

// The defaults a Policy falls back to, and what jobq run uses.
const (
	// DefaultMaxAttempts is how many times a job is attempted in all,
	// counting the first attempt.
	DefaultMaxAttempts = 3
	// DefaultDelay is the base of the backoff: the wait before the first
	// retry, before jitter.
	DefaultDelay = 200 * time.Millisecond
	// DefaultMaxBackoff caps every wait.
	DefaultMaxBackoff = 30 * time.Second
)

// Policy is a retry policy: a job is attempted up to MaxAttempts times. The
// wait before retry n (1 for the first) is Delay * 2^(n-1), capped at
// MaxBackoff, then jittered: a random point in its upper half, drawn from
// Jitter.
type Policy struct {
	// MaxAttempts caps the attempts of a job, the first one included. Zero
	// or less means DefaultMaxAttempts.
	MaxAttempts int
	// Delay is the base of the exponential backoff. Zero or less retries at
	// once.
	Delay time.Duration
	// MaxBackoff caps every wait, jitter included. Zero or less means
	// DefaultMaxBackoff.
	MaxBackoff time.Duration
	// Jitter returns a number in [0, 1). Nil means math/rand/v2's Float64;
	// tests set it to get the same waits every run.
	Jitter func() float64
}

// Default returns the policy jobq uses unless told otherwise.
func Default() Policy {
	return Policy{MaxAttempts: DefaultMaxAttempts, Delay: DefaultDelay, MaxBackoff: DefaultMaxBackoff}
}

// Attempts returns the effective cap on attempts.
func (p Policy) Attempts() int {
	if p.MaxAttempts <= 0 {
		return DefaultMaxAttempts
	}
	return p.MaxAttempts
}

// Cap returns the effective cap on a wait.
func (p Policy) Cap() time.Duration {
	if p.MaxBackoff <= 0 {
		return DefaultMaxBackoff
	}
	return p.MaxBackoff
}

// Next is called after attempt number attempt (1 for the first) of a job
// failed. It returns the wait before the next attempt and true, or false when
// the job has used all its attempts.
func (p Policy) Next(attempt int) (time.Duration, bool) {
	if attempt >= p.Attempts() {
		return 0, false
	}
	return p.Backoff(attempt), true
}

// Ceiling returns the wait after attempt before jitter: Delay doubled for
// every attempt after the first, capped.
func (p Policy) Ceiling(attempt int) time.Duration {
	if p.Delay <= 0 {
		return 0
	}
	if attempt < 1 {
		attempt = 1
	}
	limit := p.Cap()
	d := p.Delay
	for i := 1; i < attempt; i++ {
		if d >= limit/2 {
			return limit
		}
		d *= 2
	}
	return min(d, limit)
}

// Backoff returns the jittered wait after attempt: a point in the upper half
// of Ceiling(attempt), so it never exceeds the cap and never drops below half
// of the exponential step.
func (p Policy) Backoff(attempt int) time.Duration {
	c := p.Ceiling(attempt)
	if c <= 0 {
		return 0
	}
	f := p.jitter()
	if f < 0 {
		f = 0
	}
	if f >= 1 {
		f = 0.999999
	}
	half := c / 2
	return half + time.Duration(f*float64(c-half))
}

// Schedule returns the ceilings of the waits a job would see if every
// attempt failed: one per retry, so Attempts()-1 of them.
func (p Policy) Schedule() []time.Duration {
	var out []time.Duration
	for attempt := 1; attempt < p.Attempts(); attempt++ {
		out = append(out, p.Ceiling(attempt))
	}
	return out
}

// Validate reports a policy that makes no sense.
func (p Policy) Validate() error {
	if p.MaxAttempts < 0 {
		return fmt.Errorf("retry: negative max attempts %d", p.MaxAttempts)
	}
	if p.Delay < 0 {
		return fmt.Errorf("retry: negative delay %s", p.Delay)
	}
	if p.MaxBackoff < 0 {
		return fmt.Errorf("retry: negative max backoff %s", p.MaxBackoff)
	}
	return nil
}

// String describes the policy for logs.
func (p Policy) String() string {
	return fmt.Sprintf("up to %d attempts, backoff from %s to at most %s", p.Attempts(), max(p.Delay, 0), p.Cap())
}

func (p Policy) jitter() float64 {
	if p.Jitter != nil {
		return p.Jitter()
	}
	return rand.Float64()
}

// Fixed returns a Jitter that always returns f, for tests.
func Fixed(f float64) func() float64 {
	return func() float64 { return f }
}

// Seeded returns a Jitter drawing from a PCG generator with the given seeds,
// so a test sees the same sequence every run. It is not safe for concurrent
// use.
func Seeded(seed1, seed2 uint64) func() float64 {
	r := rand.New(rand.NewPCG(seed1, seed2))
	return r.Float64
}
