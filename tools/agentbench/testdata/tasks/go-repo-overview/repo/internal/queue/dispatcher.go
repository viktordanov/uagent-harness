// Package queue delivers notifications in the background, with retries.
package queue

import (
	"context"
	"log"
	"sync"
	"time"

	"example.com/shipd/internal/notify"
	"example.com/shipd/internal/store"
)

// Job is one notification to deliver.
type Job struct {
	URL     string
	Secret  string
	Event   store.Event
	attempt int
}

// Options tune a Dispatcher.
type Options struct {
	Workers     int
	MaxAttempts int
	BaseDelay   time.Duration
}

// Dispatcher runs workers that deliver jobs through a notify.Sender. A
// failed delivery is retried with exponential backoff (see backoff.go) up
// to MaxAttempts; after that the job goes to the dead-letter list.
type Dispatcher struct {
	sender notify.Sender
	opts   Options
	jobs   chan Job
	wg     sync.WaitGroup
	cancel context.CancelFunc
	dead   deadLetters
}

// NewDispatcher returns a dispatcher; Start starts its workers.
func NewDispatcher(s notify.Sender, opts Options) *Dispatcher {
	return &Dispatcher{sender: s, opts: opts, jobs: make(chan Job, 1024)}
}

// Start starts the workers.
func (d *Dispatcher) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	for range d.opts.Workers {
		d.wg.Add(1)
		go d.work(ctx)
	}
}

// Stop stops the workers; jobs still queued are dropped.
func (d *Dispatcher) Stop() {
	d.cancel()
	d.wg.Wait()
}

// Enqueue queues a job without blocking; a full queue drops the job to
// the dead-letter list.
func (d *Dispatcher) Enqueue(j Job) {
	select {
	case d.jobs <- j:
	default:
		d.dead.add(j, "queue full")
	}
}

func (d *Dispatcher) work(ctx context.Context) {
	defer d.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case j := <-d.jobs:
			d.deliver(ctx, j)
		}
	}
}

func (d *Dispatcher) deliver(ctx context.Context, j Job) {
	err := d.sender.Send(ctx, notify.Message{URL: j.URL, Secret: j.Secret, Event: j.Event})
	if err == nil {
		return
	}
	j.attempt++
	if j.attempt >= d.opts.MaxAttempts || !notify.Retryable(err) {
		d.dead.add(j, err.Error())
		log.Printf("queue: giving up on %s after %d attempts: %v", j.URL, j.attempt, err)

		return
	}
	delay := backoff(d.opts.BaseDelay, j.attempt)
	time.AfterFunc(delay, func() { d.Enqueue(j) })
}
