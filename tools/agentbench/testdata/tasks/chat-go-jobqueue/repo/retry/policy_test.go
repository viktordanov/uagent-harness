package retry

import (
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	p := Default()
	if p.Attempts() != 3 {
		t.Fatalf("Attempts() = %d, want 3", p.Attempts())
	}
	for attempt := 1; attempt <= 2; attempt++ {
		d, ok := p.Next(attempt)
		if !ok || d != DefaultDelay {
			t.Fatalf("Next(%d) = %v, %v", attempt, d, ok)
		}
	}
	if _, ok := p.Next(3); ok {
		t.Fatal("Next(3) retries past the cap")
	}
}

func TestZeroPolicyFallsBack(t *testing.T) {
	var p Policy
	if p.Attempts() != DefaultMaxAttempts {
		t.Fatalf("Attempts() = %d", p.Attempts())
	}
	if d, ok := p.Next(1); !ok || d != 0 {
		t.Fatalf("Next(1) = %v, %v", d, ok)
	}
}

func TestSchedule(t *testing.T) {
	p := Policy{MaxAttempts: 4, Delay: time.Second}
	got := p.Schedule()
	if len(got) != 3 {
		t.Fatalf("Schedule() = %v, want three waits", got)
	}
	for _, d := range got {
		if d != time.Second {
			t.Fatalf("Schedule() = %v", got)
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
	if err := (Policy{MaxAttempts: -1}).Validate(); err == nil {
		t.Fatal("negative attempts accepted")
	}
	if err := (Policy{Delay: -time.Second}).Validate(); err == nil {
		t.Fatal("negative delay accepted")
	}
}

func TestString(t *testing.T) {
	if got, want := Default().String(), "up to 3 attempts, 200ms apart"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
