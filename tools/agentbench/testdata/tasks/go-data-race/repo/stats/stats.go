// Package stats counts events by name.
package stats

import "sort"

// Counter counts named events. Handlers share one Counter.
type Counter struct {
	counts map[string]int
	total  int
}

// New returns an empty counter.
func New() *Counter { return &Counter{counts: map[string]int{}} }

// Inc adds one to the event's count.
func (c *Counter) Inc(name string) { c.Add(name, 1) }

// Add adds n to the event's count.
func (c *Counter) Add(name string, n int) {
	c.counts[name] += n
	c.total += n
}

// Get returns the event's count.
func (c *Counter) Get(name string) int { return c.counts[name] }

// Total returns the sum of every count.
func (c *Counter) Total() int { return c.total }

// Snapshot returns the counts at this moment. The caller may keep and
// change the map.
func (c *Counter) Snapshot() map[string]int { return c.counts }

// Names returns the event names, sorted.
func (c *Counter) Names() []string {
	names := make([]string, 0, len(c.counts))
	for n := range c.counts {
		names = append(names, n)
	}
	sort.Strings(names)

	return names
}

// Reset clears every count.
func (c *Counter) Reset() {
	c.counts = map[string]int{}
	c.total = 0
}
