package api

import (
	"container/list"
	"sync"
)

// idempotency remembers the last n Idempotency-Key values (an LRU), so a
// client's retry of POST /v1/events does not store the event twice.
type idempotency struct {
	mu    sync.Mutex
	max   int
	order *list.List
	keys  map[string]*list.Element
}

func newIdempotency(n int) *idempotency {
	return &idempotency{max: n, order: list.New(), keys: map[string]*list.Element{}}
}

func (c *idempotency) Seen(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.keys[key]
	if ok {
		c.order.MoveToFront(e)
	}

	return ok
}

func (c *idempotency) Remember(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.keys[key] = c.order.PushFront(key)
	if c.order.Len() > c.max {
		last := c.order.Back()
		c.order.Remove(last)
		delete(c.keys, last.Value.(string))
	}
}
