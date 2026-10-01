// Package cache is a small string cache that counts hits and misses.
package cache

import "sync"

// Cache maps keys to values. It is safe for concurrent use.
type Cache struct {
	mu     sync.RWMutex
	items  map[string]string
	hits   int
	misses int
}

// New returns an empty cache.
func New() *Cache {
	return &Cache{items: map[string]string{}}
}

// Get returns the value for key and whether it was present.
func (c *Cache) Get(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.items[key]
	if ok {
		c.hits++
	} else {
		c.misses++
	}

	return v, ok
}

// Set stores a value.
func (c *Cache) Set(key, value string) {
	c.items[key] = value
}

// Stats returns the hit and miss counts.
func (c *Cache) Stats() (hits, misses int) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.hits, c.misses
}
