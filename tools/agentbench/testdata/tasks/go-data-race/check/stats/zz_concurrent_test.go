package stats

import (
	"fmt"
	"sync"
	"testing"
)

func TestConcurrentUse(t *testing.T) {
	c := New()
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				c.Inc(fmt.Sprint("e", i%7))
				if i%50 == 0 {
					s := c.Snapshot()
					s["mine"]++
					_ = c.Names()
					_ = c.Get("e1")
					_ = c.Total()
				}
			}
		}(g)
	}
	wg.Wait()
	if c.Total() != 16*2000 {
		t.Fatalf("Total = %d, want %d", c.Total(), 16*2000)
	}
	if c.Get("mine") != 0 {
		t.Fatalf("a snapshot changed the counter: mine = %d", c.Get("mine"))
	}
	sum := 0
	for _, n := range c.Snapshot() {
		sum += n
	}
	if sum != c.Total() {
		t.Fatalf("snapshot sum %d != total %d", sum, c.Total())
	}
}
