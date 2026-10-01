package queue

import (
	"testing"
	"time"
)

func TestBackoff(t *testing.T) {
	if got := backoff(100*time.Millisecond, 3); got != 400*time.Millisecond {
		t.Fatalf("backoff = %v", got)
	}
	if got := backoff(time.Second, 40); got != maxDelay {
		t.Fatalf("backoff = %v, want the cap", got)
	}
}
