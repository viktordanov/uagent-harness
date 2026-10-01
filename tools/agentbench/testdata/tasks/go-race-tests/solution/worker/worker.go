// Package worker runs a function repeatedly until it is stopped.
package worker

import (
	"sync"
	"sync/atomic"
	"time"
)

// Worker calls its function every interval until Stop.
type Worker struct {
	fn       func()
	interval time.Duration
	stopped  atomic.Bool
	runs     atomic.Int64
	done     chan struct{}
	once     sync.Once
}

// New returns a worker for fn; it does not start.
func New(fn func(), interval time.Duration) *Worker {
	return &Worker{fn: fn, interval: interval, done: make(chan struct{})}
}

// Start runs the loop in the background.
func (w *Worker) Start() {
	go func() {
		defer close(w.done)
		for !w.stopped.Load() {
			w.fn()
			w.runs.Add(1)
			time.Sleep(w.interval)
		}
	}()
}

// Stop asks the loop to end and waits for it.
func (w *Worker) Stop() {
	w.once.Do(func() { w.stopped.Store(true) })
	<-w.done
}

// Runs reports how many times the function ran.
func (w *Worker) Runs() int {
	return int(w.runs.Load())
}
