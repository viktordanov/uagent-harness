package store

import (
	"strconv"
	"sync"
	"testing"

	"example.com/jobq/internal/clock"
	"example.com/jobq/queue"
)

// TestClaimIsExclusive claims from many goroutines at once: every job must be
// claimed exactly once. Run with -race.
func TestClaimIsExclusive(t *testing.T) {
	const jobs, workers = 400, 16
	m := NewMemory()
	for i := 0; i < jobs; i++ {
		if _, err := m.Add(queue.NewJob("echo", strconv.Itoa(i))); err != nil {
			t.Fatal(err)
		}
	}
	var (
		mu     sync.Mutex
		claims = make(map[string]int)
		wg     sync.WaitGroup
		start  = make(chan struct{})
	)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for {
				j, ok, err := m.Claim(clock.Epoch)
				if err != nil {
					t.Error(err)
					return
				}
				if !ok {
					return
				}
				mu.Lock()
				claims[j.ID]++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()
	if len(claims) != jobs {
		t.Fatalf("%d jobs claimed, want %d", len(claims), jobs)
	}
	for id, n := range claims {
		if n != 1 {
			t.Errorf("%s claimed %d times", id, n)
		}
	}
}
