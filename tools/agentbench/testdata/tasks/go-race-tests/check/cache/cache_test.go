package cache

import (
	"fmt"
	"sync"
	"testing"
)

func TestConcurrentUse(t *testing.T) {
	c := New()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				k := fmt.Sprintf("k%d", i%20)
				c.Set(k, fmt.Sprint(g))
				c.Get(k)
			}
		}(g)
	}
	wg.Wait()
	hits, misses := c.Stats()
	if hits+misses != 8*200 {
		t.Fatalf("hits+misses = %d, want %d", hits+misses, 8*200)
	}
}
