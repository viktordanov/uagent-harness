// Package metrics counts what a worker pool does: jobs claimed, finished,
// retried and failed, in all and per kind.
package metrics

import (
	"sort"
	"sync"
	"time"
)

// Counters is safe for concurrent use. A nil *Counters counts nothing, so a
// pool without metrics need not check.
type Counters struct {
	mu      sync.Mutex
	claimed int
	kinds   map[string]*KindCounts
}

// KindCounts are the counts of one kind of job.
type KindCounts struct {
	Claimed   int           `json:"claimed"`
	Succeeded int           `json:"succeeded"`
	Retried   int           `json:"retried"`
	Failed    int           `json:"failed"`
	Busy      time.Duration `json:"busy"` // time spent in successful attempts
}

// New returns zeroed counters.
func New() *Counters {
	return &Counters{kinds: make(map[string]*KindCounts)}
}

func (c *Counters) kind(k string) *KindCounts {
	kc, ok := c.kinds[k]
	if !ok {
		kc = &KindCounts{}
		c.kinds[k] = kc
	}
	return kc
}

// Claimed counts a job of kind k claimed by a worker.
func (c *Counters) Claimed(k string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.claimed++
	c.kind(k).Claimed++
}

// Succeeded counts a successful attempt of kind k that took d.
func (c *Counters) Succeeded(k string, d time.Duration) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	kc := c.kind(k)
	kc.Succeeded++
	kc.Busy += d
}

// Retried counts a failed attempt of kind k that will be retried.
func (c *Counters) Retried(k string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.kind(k).Retried++
}

// Failed counts a job of kind k that failed for good.
func (c *Counters) Failed(k string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.kind(k).Failed++
}

// Snapshot is a copy of the counters at one moment.
type Snapshot struct {
	Claimed   int                   `json:"claimed"`
	Succeeded int                   `json:"succeeded"`
	Retried   int                   `json:"retried"`
	Failed    int                   `json:"failed"`
	Kinds     map[string]KindCounts `json:"kinds"`
}

// Snapshot returns the current counts.
func (c *Counters) Snapshot() Snapshot {
	s := Snapshot{Kinds: make(map[string]KindCounts)}
	if c == nil {
		return s
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	s.Claimed = c.claimed
	for k, kc := range c.kinds {
		s.Kinds[k] = *kc
		s.Succeeded += kc.Succeeded
		s.Retried += kc.Retried
		s.Failed += kc.Failed
	}
	return s
}

// KindNames returns the kinds in s, sorted.
func (s Snapshot) KindNames() []string {
	names := make([]string, 0, len(s.Kinds))
	for k := range s.Kinds {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
