package worker

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"example.com/jobq/internal/clock"
	"example.com/jobq/metrics"
	"example.com/jobq/queue"
	"example.com/jobq/retry"
)

// DefaultPoll is how long an idle worker waits before it looks for a ready
// job again, while some job is still waiting for its retry.
const DefaultPoll = 20 * time.Millisecond

// Config configures a Pool. The zero value is one worker, the default retry
// policy and the wall clock.
type Config struct {
	// Workers is the number of goroutines that run jobs; at least 1.
	Workers int
	// Policy decides whether a failed attempt is retried, and when.
	Policy retry.Policy
	// Poll is the wait of an idle worker; DefaultPoll when zero.
	Poll time.Duration
	// JobTimeout bounds one attempt; zero means no bound.
	JobTimeout time.Duration
	// Clock is the pool's time source; the queue's clock when nil.
	Clock clock.Clock
	// Metrics, if not nil, counts what the pool does.
	Metrics *metrics.Counters
	// Logf, if not nil, receives one line per failed attempt.
	Logf func(format string, args ...any)
}

// Pool runs the jobs of a queue until the queue is idle.
type Pool struct {
	q   *queue.Queue
	reg *Registry
	cfg Config
}

// New returns a pool that runs the jobs of q with the handlers in reg.
func New(q *queue.Queue, reg *Registry, cfg Config) *Pool {
	if cfg.Workers < 1 {
		cfg.Workers = 1
	}
	if cfg.Poll <= 0 {
		cfg.Poll = DefaultPoll
	}
	if cfg.Clock == nil {
		cfg.Clock = q.Clock()
	}
	return &Pool{q: q, reg: reg, cfg: cfg}
}

// Run starts the workers and waits for them. A worker stops when the queue is
// idle: no job is pending, including the ones waiting for a retry, and none is
// running. Run returns the first store error, which stops every worker, or
// ctx.Err() when ctx ended the run.
func (p *Pool) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg       sync.WaitGroup
		once     sync.Once
		firstErr error
	)
	for i := 0; i < p.cfg.Workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if err := p.loop(ctx, id); err != nil {
				once.Do(func() {
					firstErr = err
					cancel()
				})
			}
		}(i + 1)
	}
	wg.Wait()
	if firstErr != nil && !errors.Is(firstErr, context.Canceled) {
		return firstErr
	}
	return ctx.Err()
}

// loop is one worker: claim a ready job and run it, or wait a poll interval
// when none is ready, until the queue is idle.
func (p *Pool) loop(ctx context.Context, id int) error {
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		t, ok, err := p.q.Claim()
		if err != nil {
			return fmt.Errorf("worker %d: claim: %w", id, err)
		}
		if !ok {
			idle, err := p.q.Idle()
			if err != nil {
				return fmt.Errorf("worker %d: %w", id, err)
			}
			if idle {
				return nil
			}
			if err := p.cfg.Clock.Sleep(ctx, p.cfg.Poll); err != nil {
				return nil
			}
			continue
		}
		if err := p.process(ctx, t); err != nil {
			return fmt.Errorf("worker %d: %w", id, err)
		}
	}
}

// process runs one attempt of t and records its outcome in the queue. The
// error it returns is the queue's, not the handler's.
func (p *Pool) process(ctx context.Context, t queue.Job) error {
	p.cfg.Metrics.Claimed(t.Kind)
	h, ok := p.reg.Lookup(t.Kind)
	if !ok {
		p.cfg.Metrics.Failed(t.Kind)
		return p.q.Fail(t, fmt.Errorf("no handler for kind %q", t.Kind))
	}

	start := p.cfg.Clock.Now()
	err := p.call(ctx, h, t)
	if err == nil {
		p.cfg.Metrics.Succeeded(t.Kind, clock.Since(p.cfg.Clock, start))
		return p.q.Complete(t)
	}
	if ctx.Err() != nil {
		// The run is stopping, not the job failing: put it back for the
		// next run without a delay.
		return p.q.Retry(t, err, 0)
	}
	if delay, again := p.cfg.Policy.Next(t.Attempts); again {
		p.cfg.Metrics.Retried(t.Kind)
		p.logf("%s attempt %d failed: %v; retrying in %s", t.ID, t.Attempts, err, delay)
		return p.q.Retry(t, err, delay)
	}
	p.cfg.Metrics.Failed(t.Kind)
	p.logf("%s failed after %d attempts: %v", t.ID, t.Attempts, err)
	return p.q.Fail(t, err)
}

// call runs h on t with the job timeout, and turns a panic into an error.
func (p *Pool) call(ctx context.Context, h Handler, t queue.Job) (err error) {
	if p.cfg.JobTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.cfg.JobTimeout)
		defer cancel()
	}
	defer func() {
		if r := recover(); r != nil {
			err = &PanicError{Value: r, Stack: debug.Stack()}
		}
	}()
	return h(ctx, t)
}

func (p *Pool) logf(format string, args ...any) {
	if p.cfg.Logf != nil {
		p.cfg.Logf(format, args...)
	}
}

// PanicError is the error of an attempt whose handler panicked.
type PanicError struct {
	Value any
	Stack []byte
}

func (e *PanicError) Error() string {
	return fmt.Sprintf("handler panicked: %v", e.Value)
}
