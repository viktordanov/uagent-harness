package clock

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFakeStartsAtEpoch(t *testing.T) {
	f := NewFake(time.Time{})
	if !f.Now().Equal(Epoch) {
		t.Fatalf("Now() = %v, want %v", f.Now(), Epoch)
	}
}

func TestFakeAdvanceAndSet(t *testing.T) {
	f := NewFake(Epoch)
	f.Advance(90 * time.Second)
	if got := f.Now().Sub(Epoch); got != 90*time.Second {
		t.Fatalf("after Advance: %v since epoch", got)
	}
	f.Advance(-time.Hour)
	if got := f.Now().Sub(Epoch); got != 90*time.Second {
		t.Fatalf("negative Advance moved the clock: %v", got)
	}
	f.Set(Epoch) // earlier: ignored
	if got := f.Now().Sub(Epoch); got != 90*time.Second {
		t.Fatalf("Set to the past moved the clock: %v", got)
	}
	f.Set(Epoch.Add(time.Hour))
	if got := f.Now().Sub(Epoch); got != time.Hour {
		t.Fatalf("after Set: %v", got)
	}
}

func TestFakeSleepRecordsAndAdvances(t *testing.T) {
	f := NewFake(Epoch)
	ctx := context.Background()
	for _, d := range []time.Duration{time.Second, 0, 3 * time.Second} {
		if err := f.Sleep(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.Now().Sub(Epoch); got != 4*time.Second {
		t.Fatalf("slept %v in total, want 4s", got)
	}
	want := []time.Duration{time.Second, 0, 3 * time.Second}
	got := f.Sleeps()
	if len(got) != len(want) {
		t.Fatalf("Sleeps() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Sleeps() = %v, want %v", got, want)
		}
	}
	f.ClearSleeps()
	if len(f.Sleeps()) != 0 {
		t.Fatal("ClearSleeps kept sleeps")
	}
}

func TestFakeSleepCancelled(t *testing.T) {
	f := NewFake(Epoch)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.Sleep(ctx, time.Minute); !errors.Is(err, context.Canceled) {
		t.Fatalf("Sleep on a done context: %v", err)
	}
	if !f.Now().Equal(Epoch) {
		t.Fatal("a cancelled Sleep advanced the clock")
	}
}

func TestRealSleepCancelled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := Real{}.Sleep(ctx, time.Minute)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Sleep: %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("Sleep ignored the context")
	}
}

func TestDeadline(t *testing.T) {
	f := NewFake(Epoch)
	if !Deadline(f, 0).IsZero() {
		t.Fatal("Deadline(0) is not the zero time")
	}
	if got := Deadline(f, time.Minute); !got.Equal(Epoch.Add(time.Minute)) {
		t.Fatalf("Deadline(1m) = %v", got)
	}
	f.Advance(time.Second)
	if got := Since(f, Epoch); got != time.Second {
		t.Fatalf("Since = %v", got)
	}
}
