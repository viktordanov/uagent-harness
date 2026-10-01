package quota

import "sync"

// CounterStore counts requests per tenant. Handlers call it from many
// goroutines at once.
type CounterStore struct {
	mu     sync.Mutex
	counts map[string]int
}

// NewCounterStore returns an empty store.
func NewCounterStore() *CounterStore {
	return &CounterStore{counts: map[string]int{}}
}

// Get returns the tenant's count.
func (s *CounterStore) Get(tenant string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.counts[tenant]
}

// Increment adds one to the tenant's count.
func (s *CounterStore) Increment(tenant string) {
	s.counts[tenant]++
}

// Reset clears every count.
func (s *CounterStore) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counts = map[string]int{}
}
