package store_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"example.com/jobq/queue"
	"example.com/jobq/store"
)

// The agentbench check.

func TestHiddenClaimExactlyOnce(t *testing.T) {
	const jobs, workers = 300, 16
	now := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	for round := 0; round < 8; round++ {
		m := store.NewMemory()
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
					j, ok, err := m.Claim(now)
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
			t.Fatalf("round %d: %d distinct jobs claimed, want %d", round, len(claims), jobs)
		}
		for id, n := range claims {
			if n != 1 {
				t.Fatalf("round %d: %s claimed %d times", round, id, n)
			}
			j, err := m.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			if j.Attempts != 1 || j.State != queue.Running {
				t.Fatalf("round %d: %s after one claim: %+v", round, id, j)
			}
		}
	}
}

func TestHiddenFileKeepsDead(t *testing.T) {
	dead, err := queue.ParseState("dead")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "jobs.json")
	f, err := store.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	j, err := f.Add(queue.NewJob("fail", ""))
	if err != nil {
		t.Fatal(err)
	}
	keep, err := f.Add(queue.NewJob("echo", "x"))
	if err != nil {
		t.Fatal(err)
	}
	j.State = dead
	j.Attempts = 3
	j.LastError = "boom"
	if err := f.Update(j); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `"dead"`) {
		t.Fatalf("store file has no dead job:\n%s", b)
	}

	g, err := store.OpenFile(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, err := g.Get(j.ID)
	if err != nil || got.State != dead || got.Attempts != 3 || got.LastError != "boom" {
		t.Fatalf("reopened dead job: %+v, %v", got, err)
	}
	list, err := g.List(dead)
	if err != nil || len(list) != 1 || list[0].ID != j.ID {
		t.Fatalf("List(dead) = %v, %v", list, err)
	}
	counts, err := g.Counts()
	if err != nil || counts[dead] != 1 || counts[queue.Pending] != 1 {
		t.Fatalf("Counts() = %v, %v", counts, err)
	}
	if other, _ := g.Get(keep.ID); other.State != queue.Pending {
		t.Fatalf("other job: %+v", other)
	}
}
