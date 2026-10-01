// Package worker runs a function repeatedly until it is stopped.
package worker

import (
	"sync"
	"time"
)

// Worker calls its function every interval until Stop.
type Worker struct {
	fn       func()
	interval time.Duration
	stopped  bool
	runs     int
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
		for !w.stopped {
			w.fn()
			w.runs++
			time.Sleep(w.interval)
		}
	}()
}

// Stop asks the loop to end and waits for it.
func (w *Worker) Stop() {
	w.once.Do(func() { w.stopped = true })
	<-w.done
}

// Runs reports how many times the function ran.
func (w *Worker) Runs() int {
	return w.runs
}
