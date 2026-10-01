// Package quota limits how many requests a tenant may make per window.
package quota

// Quota allows each tenant at most Limit requests until Reset.
type Quota struct {
	limit int
	store *CounterStore
}

// New returns a quota of limit requests per tenant.
func New(limit int) *Quota {
	return &Quota{limit: limit, store: NewCounterStore()}
}

// Allow reports whether the tenant may make one more request, and counts
// it when it may.
func (q *Quota) Allow(tenant string) bool {
	used := q.store.Get(tenant)
	if used > q.limit {
		return false
	}
	q.store.Increment(tenant)

	return true
}

// Remaining is how many requests the tenant has left.
func (q *Quota) Remaining(tenant string) int {
	return max(0, q.limit-q.store.Get(tenant))
}

// Reset starts a new window for every tenant.
func (q *Quota) Reset() {
	q.store.Reset()
}
