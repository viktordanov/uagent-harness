package retry

import (
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	p := Default()
	p.Jitter = Fixed(0.999999)
	if p.Attempts() != 3 {
		t.Fatalf("Attempts() = %d, want 3", p.Attempts())
	}
	for attempt := 1; attempt <= 2; attempt++ {
		d, ok := p.Next(attempt)
		ceil := DefaultDelay << (attempt - 1)
		if !ok || d > ceil || d < ceil/2 {
			t.Fatalf("Next(%d) = %v, %v; want within [%v, %v]", attempt, d, ok, ceil/2, ceil)
		}
	}
	if _, ok := p.Next(3); ok {
		t.Fatal("Next(3) retries past the cap")
	}
}

func TestZeroPolicyFallsBack(t *testing.T) {
	var p Policy
	if p.Attempts() != DefaultMaxAttempts || p.Cap() != DefaultMaxBackoff {
		t.Fatalf("Attempts() = %d, Cap() = %v", p.Attempts(), p.Cap())
	}
	if d, ok := p.Next(1); !ok || d != 0 {
		t.Fatalf("Next(1) = %v, %v", d, ok)
	}
}

func TestExponentialCeiling(t *testing.T) {
	p := Policy{MaxAttempts: 10, Delay: 100 * time.Millisecond, MaxBackoff: time.Second}
	want := []time.Duration{100, 200, 400, 800, 1000, 1000}
	for i, w := range want {
		if got := p.Ceiling(i + 1); got != w*time.Millisecond {
			t.Errorf("Ceiling(%d) = %v, want %v", i+1, got, w*time.Millisecond)
		}
	}
	// No overflow far out.
	if got := p.Ceiling(200); got != time.Second {
		t.Errorf("Ceiling(200) = %v", got)
	}
}

func TestJitterIsDeterministicAndBounded(t *testing.T) {
	p := Policy{MaxAttempts: 50, Delay: 10 * time.Millisecond, MaxBackoff: 300 * time.Millisecond, Jitter: Fixed(0)}
	if d, _ := p.Next(1); d != 5*time.Millisecond {
		t.Fatalf("jitter 0: Next(1) = %v, want 5ms", d)
	}
	p.Jitter = Fixed(0.5)
	if d, _ := p.Next(2); d != 15*time.Millisecond {
		t.Fatalf("jitter 0.5: Next(2) = %v, want 15ms", d)
	}

	a, b := p, p
	a.Jitter, b.Jitter = Seeded(1, 2), Seeded(1, 2)
	for attempt := 1; attempt < 50; attempt++ {
		da, _ := a.Next(attempt)
		db, _ := b.Next(attempt)
		if da != db {
			t.Fatalf("same seed, attempt %d: %v and %v", attempt, da, db)
		}
		if da > p.MaxBackoff || da < p.Ceiling(attempt)/2 {
			t.Fatalf("attempt %d: %v outside [%v, %v]", attempt, da, p.Ceiling(attempt)/2, p.MaxBackoff)
		}
	}
}

func TestSchedule(t *testing.T) {
	p := Policy{MaxAttempts: 4, Delay: time.Second, MaxBackoff: 3 * time.Second}
	got := p.Schedule()
	want := []time.Duration{time.Second, 2 * time.Second, 3 * time.Second}
	if len(got) != len(want) {
		t.Fatalf("Schedule() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Schedule() = %v, want %v", got, want)
		}
	}
	if s := (Policy{MaxAttempts: 1}).Schedule(); len(s) != 0 {
		t.Fatalf("one attempt has waits: %v", s)
	}
}

func TestValidate(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatal(err)
	}
	for _, p := range []Policy{{MaxAttempts: -1}, {Delay: -time.Second}, {MaxBackoff: -time.Second}} {
		if err := p.Validate(); err == nil {
			t.Errorf("%+v accepted", p)
		}
	}
}

func TestString(t *testing.T) {
	if got, want := Default().String(), "up to 3 attempts, backoff from 200ms to at most 30s"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
