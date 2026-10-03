// Package cachestats accounts for a session's prompt cache: per request,
// the input the provider could have served from its cache, what it did
// serve, and why the rest missed. The provider keeps a prompt cache per
// model and request effort, and lets it expire after some idle minutes, so
// a session that switches effort, switches model, pauses, or rewrites its
// history pays for input it already sent. An effort update (a
// configuration_update item) leaves the request's effort, and the cache, as
// they are. See docs/design/adaptive-effort-costs.md.
package cachestats

// Block is the provider's prompt cache granularity in tokens.
const Block = 128

// Cache models the provider's prompt cache over one session: each key (a
// model and effort) keeps the longest prompt sent at it, and a request
// finds cached the part of its input that prompt covers, in whole blocks.
// A rewritten history (a compaction) leaves every key only the base, the
// prefix shared across sessions (the instructions and tools).
type Cache struct {
	base   int64
	prefix map[string]int64
}

// NewCache is an empty cache over base, the cross-session prefix.
func NewCache(base int64) *Cache { return &Cache{base: base, prefix: map[string]int64{}} }

// Hit is what a request of input tokens at key finds cached.
func (c *Cache) Hit(key string, input int64) int64 {
	return min(input, c.at(key)) / Block * Block
}

// Longest is what a request of input tokens finds cached at the best key:
// what one cache for every key would have served.
func (c *Cache) Longest(input int64) int64 {
	best := c.base
	for _, p := range c.prefix {
		best = max(best, p)
	}

	return min(input, best) / Block * Block
}

// Sent records a request of input tokens at key.
func (c *Cache) Sent(key string, input int64) { c.prefix[key] = max(c.at(key), input) }

// Reset leaves every key the base.
func (c *Cache) Reset() { clear(c.prefix) }

func (c *Cache) at(key string) int64 {
	if p, ok := c.prefix[key]; ok {
		return p
	}

	return c.base
}
