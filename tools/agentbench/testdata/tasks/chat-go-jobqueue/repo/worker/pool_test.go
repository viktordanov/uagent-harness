package worker_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"example.com/jobq/internal/clock"
	"example.com/jobq/metrics"
	"example.com/jobq/queue"
	"example.com/jobq/retry"
	"example.com/jobq/store"
	"example.com/jobq/worker"
)

type harness struct {
	clk  *clock.Fake
	q    *queue.Queue
	reg  *worker.Registry
	m    *metrics.Counters
	mu   sync.Mutex
	logs []string
}

func newHarness() *harness {
	clk := clock.NewFake(clock.Epoch)
	return &harness{
		clk: clk,
		q:   queue.New(store.NewMemory(), clk),
		reg: worker.NewRegistry(),
		m:   metrics.New(),
	}
}

func (h *harness) run(t *testing.T, cfg worker.Config) {
	t.Helper()
	cfg.Metrics = h.m
	cfg.Logf = func(format string, args ...any) {
		h.mu.Lock()
		h.logs = append(h.logs, fmt.Sprintf(format, args...))
		h.mu.Unlock()
	}
	if err := worker.New(h.q, h.reg, cfg).Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func (h *harness) enqueue(t *testing.T, kind, payload string) queue.Task {
	t.Helper()
	task, err := h.q.Enqueue(kind, payload, 0)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func (h *harness) get(t *testing.T, id string) queue.Task {
	t.Helper()
	task, err := h.q.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func TestRunCompletesJobs(t *testing.T) {
	h := newHarness()
	var mu sync.Mutex
	var seen []string
	h.reg.Register("echo", func(_ context.Context, task queue.Task) error {
		mu.Lock()
		seen = append(seen, task.Payload)
		mu.Unlock()
		return nil
	})
	a := h.enqueue(t, "echo", "a")
	b := h.enqueue(t, "echo", "b")
	h.run(t, worker.Config{Policy: retry.Default()})

	if len(seen) != 2 || seen[0] != "a" || seen[1] != "b" {
		t.Fatalf("handled %v", seen)
	}
	for _, id := range []string{a.ID, b.ID} {
		if got := h.get(t, id); got.State != queue.Done || got.Attempts != 1 {
			t.Errorf("%s: %+v", id, got)
		}
	}
	if s := h.m.Snapshot(); s.Claimed != 2 || s.Succeeded != 2 {
		t.Errorf("metrics: %+v", s)
	}
}

func TestRunRetriesThenSucceeds(t *testing.T) {
	h := newHarness()
	h.reg.Register("flaky", func(_ context.Context, task queue.Task) error {
		if task.Attempts < 3 {
			return errors.New("not yet")
		}
		return nil
	})
	job := h.enqueue(t, "flaky", "")
	start := h.clk.Now()
	h.run(t, worker.Config{Policy: retry.Policy{MaxAttempts: 3, Delay: time.Second}})

	got := h.get(t, job.ID)
	if got.State != queue.Done || got.Attempts != 3 || got.LastError != "" {
		t.Fatalf("job: %+v", got)
	}
	// Two retries, a second apart: the fake clock moved at least 2s.
	if elapsed := h.clk.Now().Sub(start); elapsed < 2*time.Second {
		t.Fatalf("the retries did not wait: %v", elapsed)
	}
	if s := h.m.Snapshot(); s.Retried != 2 || s.Succeeded != 1 || s.Failed != 0 {
		t.Errorf("metrics: %+v", s)
	}
	if len(h.logs) != 2 || !strings.Contains(h.logs[0], "retrying in 1s") {
		t.Errorf("logs: %q", h.logs)
	}
}

func TestRunGivesUpAfterMaxAttempts(t *testing.T) {
	h := newHarness()
	h.reg.Register("fail", func(context.Context, queue.Task) error {
		return errors.New("boom")
	})
	job := h.enqueue(t, "fail", "")
	h.run(t, worker.Config{Policy: retry.Default()})

	got := h.get(t, job.ID)
	if got.State != queue.Failed || got.Attempts != retry.DefaultMaxAttempts || got.LastError != "boom" {
		t.Fatalf("job: %+v", got)
	}
	if s := h.m.Snapshot(); s.Failed != 1 || s.Retried != 2 {
		t.Errorf("metrics: %+v", s)
	}
}

func TestRunUnknownKindFails(t *testing.T) {
	h := newHarness()
	job := h.enqueue(t, "nobody", "")
	h.run(t, worker.Config{})
	got := h.get(t, job.ID)
	if got.State != queue.Failed || !strings.Contains(got.LastError, "no handler") {
		t.Fatalf("job: %+v", got)
	}
}

func TestRunRecoversPanics(t *testing.T) {
	h := newHarness()
	h.reg.Register("panic", func(context.Context, queue.Task) error {
		panic("oh no")
	})
	job := h.enqueue(t, "panic", "")
	h.run(t, worker.Config{Policy: retry.Policy{MaxAttempts: 1}})
	got := h.get(t, job.ID)
	if got.State != queue.Failed || !strings.Contains(got.LastError, "oh no") {
		t.Fatalf("job: %+v", got)
	}
}

func TestRunJobTimeout(t *testing.T) {
	h := newHarness()
	h.reg.Register("slow", func(ctx context.Context, _ queue.Task) error {
		<-ctx.Done()
		return ctx.Err()
	})
	job := h.enqueue(t, "slow", "")
	h.run(t, worker.Config{Policy: retry.Policy{MaxAttempts: 1}, JobTimeout: 5 * time.Millisecond})
	got := h.get(t, job.ID)
	if got.State != queue.Failed || !strings.Contains(got.LastError, "deadline") {
		t.Fatalf("job: %+v", got)
	}
}

func TestRunWaitsForDelayedJobs(t *testing.T) {
	h := newHarness()
	h.reg.Register("echo", func(context.Context, queue.Task) error { return nil })
	job, err := h.q.Enqueue("echo", "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	h.run(t, worker.Config{Poll: 10 * time.Second})
	if got := h.get(t, job.ID); got.State != queue.Done {
		t.Fatalf("job: %+v", got)
	}
	if h.clk.Now().Sub(clock.Epoch) < time.Minute {
		t.Fatal("the job ran before its time")
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	h := newHarness()
	h.reg.Register("echo", func(context.Context, queue.Task) error { return nil })
	if _, err := h.q.Enqueue("echo", "", time.Hour); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := worker.New(h.q, h.reg, worker.Config{}).Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run on a cancelled context: %v", err)
	}
}

func TestRegistry(t *testing.T) {
	r := worker.NewRegistry()
	r.Register("b", func(context.Context, queue.Task) error { return nil })
	r.Register("a", func(context.Context, queue.Task) error { return nil })
	if k := r.Kinds(); len(k) != 2 || k[0] != "a" || k[1] != "b" {
		t.Fatalf("Kinds() = %v", k)
	}
	if _, ok := r.Lookup("c"); ok {
		t.Fatal("Lookup found an unregistered kind")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("registering a kind twice did not panic")
		}
	}()
	r.Register("a", func(context.Context, queue.Task) error { return nil })
}
