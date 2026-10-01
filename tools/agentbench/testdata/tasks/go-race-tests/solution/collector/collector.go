// Package collector gathers results from concurrent producers.
package collector

import "sync"

// Result is one producer's output.
type Result struct {
	Source string
	Value  int
}

// Collect runs each producer in its own goroutine and returns every
// result they produce, in no particular order.
func Collect(producers map[string]func() []int) []Result {
	var out []Result
	var mu sync.Mutex
	var wg sync.WaitGroup
	for name, p := range producers {
		wg.Add(1)
		go func(name string, p func() []int) {
			defer wg.Done()
			vals := p()
			mu.Lock()
			defer mu.Unlock()
			for _, v := range vals {
				out = append(out, Result{Source: name, Value: v})
			}
		}(name, p)
	}
	wg.Wait()

	return out
}
