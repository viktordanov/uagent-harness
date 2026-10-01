package worker

import (
	"testing"
	"time"
)

func TestStopAndCount(t *testing.T) {
	w := New(func() {}, time.Millisecond)
	w.Start()
	deadline := time.Now().Add(2 * time.Second)
	for w.Runs() < 5 {
		if time.Now().After(deadline) {
			t.Fatal("worker did not run")
		}
		time.Sleep(time.Millisecond)
	}
	w.Stop()
	n := w.Runs()
	time.Sleep(10 * time.Millisecond)
	if w.Runs() != n {
		t.Fatalf("worker kept running after Stop: %d then %d", n, w.Runs())
	}
}
