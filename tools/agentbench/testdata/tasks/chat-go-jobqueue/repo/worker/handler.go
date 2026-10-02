// Package worker runs the jobs of a queue in a pool of goroutines, with a
// handler per kind of job and a retry policy for the attempts that fail.
package worker

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"example.com/jobq/queue"
)

// Handler runs one attempt of a task. A nil error completes the task; any
// other error fails the attempt, which the pool's retry policy may retry.
// A handler should return when ctx is done.
type Handler func(ctx context.Context, t queue.Task) error

// Registry maps a kind of job to its handler. It is safe for concurrent use.
type Registry struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{handlers: make(map[string]Handler)}
}

// Register sets the handler of kind. It panics on an empty kind, a nil
// handler, or a kind registered twice: all three are programming errors.
func (r *Registry) Register(kind string, h Handler) {
	if kind == "" || h == nil {
		panic("worker: Register needs a kind and a handler")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.handlers[kind]; dup {
		panic(fmt.Sprintf("worker: kind %q registered twice", kind))
	}
	r.handlers[kind] = h
}

// Lookup returns the handler of kind.
func (r *Registry) Lookup(kind string) (Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[kind]
	return h, ok
}

// Kinds returns the registered kinds, sorted.
func (r *Registry) Kinds() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	kinds := make([]string, 0, len(r.handlers))
	for k := range r.handlers {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	return kinds
}
