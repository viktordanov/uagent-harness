package queue_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"example.com/jobq/internal/clock"
	"example.com/jobq/queue"
	"example.com/jobq/store"
)

func newQueue() (*queue.Queue, *clock.Fake) {
	clk := clock.NewFake(clock.Epoch)
	return queue.New(store.NewMemory(), clk), clk
}

func claim(t *testing.T, q *queue.Queue) queue.Job {
	t.Helper()
	task, ok, err := q.Claim()
	if err != nil || !ok {
		t.Fatalf("Claim = %v, %v, %v", task, ok, err)
	}
	return task
}

func TestEnqueueStampsTimes(t *testing.T) {
	q, _ := newQueue()
	task, err := q.Enqueue("echo", "hi", 0)
	if err != nil {
		t.Fatal(err)
	}
	if task.ID == "" || task.State != queue.Pending {
		t.Fatalf("enqueued %+v", task)
	}
	if !task.CreatedAt.Equal(clock.Epoch) || !task.RunAt.Equal(clock.Epoch) {
		t.Fatalf("times: %+v", task)
	}
	if _, err := q.Enqueue("", "x", 0); err == nil {
		t.Fatal("Enqueue accepted an empty kind")
	}
}

func TestDelayedEnqueue(t *testing.T) {
	q, clk := newQueue()
	if _, err := q.Enqueue("echo", "", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := q.Claim(); ok {
		t.Fatal("claimed a delayed job early")
	}
	clk.Advance(time.Minute)
	claim(t, q)
}

func TestCompleteFromRunningOnly(t *testing.T) {
	q, _ := newQueue()
	task, _ := q.Enqueue("echo", "", 0)
	if err := q.Complete(task); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("Complete of a pending job: %v", err)
	}
	running := claim(t, q)
	if err := q.Complete(running); err != nil {
		t.Fatal(err)
	}
	got, _ := q.Get(task.ID)
	if got.State != queue.Done {
		t.Fatalf("state %s", got.State)
	}
	if err := q.Complete(running); err == nil {
		t.Fatal("completed a job twice")
	}
}

func TestRetryWaitsForDelay(t *testing.T) {
	q, clk := newQueue()
	task, _ := q.Enqueue("echo", "", 0)
	running := claim(t, q)
	if err := q.Retry(running, errors.New("flaky"), 5*time.Second); err != nil {
		t.Fatal(err)
	}
	got, _ := q.Get(task.ID)
	if got.State != queue.Pending || got.LastError != "flaky" || got.Attempts != 1 {
		t.Fatalf("after Retry: %+v", got)
	}
	if _, ok, _ := q.Claim(); ok {
		t.Fatal("claimed before the retry delay")
	}
	clk.Advance(5 * time.Second)
	again := claim(t, q)
	if again.Attempts != 2 {
		t.Fatalf("second claim: %+v", again)
	}
}

func TestFailAndRequeue(t *testing.T) {
	q, _ := newQueue()
	task, _ := q.Enqueue("echo", "", 0)
	running := claim(t, q)
	if err := q.Fail(running, errors.New("gave up")); err != nil {
		t.Fatal(err)
	}
	got, _ := q.Get(task.ID)
	if got.State != queue.Dead || got.LastError != "gave up" {
		t.Fatalf("after Fail: %+v", got)
	}
	back, err := q.Requeue(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.State != queue.Pending || back.Attempts != 0 {
		t.Fatalf("after Requeue: %+v", back)
	}
	if _, err := q.Requeue(task.ID); err == nil {
		t.Fatal("requeued a pending job")
	}
	if _, err := q.Requeue("job-9999"); !errors.Is(err, queue.ErrNotFound) {
		t.Fatalf("Requeue of a missing job: %v", err)
	}
}

func TestRecoverRunning(t *testing.T) {
	q, _ := newQueue()
	q.Enqueue("echo", "a", 0)
	q.Enqueue("echo", "b", 0)
	claim(t, q)
	n, err := q.RecoverRunning()
	if err != nil || n != 1 {
		t.Fatalf("RecoverRunning = %d, %v", n, err)
	}
	pending, _ := q.List(queue.Pending)
	if len(pending) != 2 {
		t.Fatalf("pending after recovery: %v", pending)
	}
	if pending[0].Attempts != 1 || pending[0].LastError != "interrupted" {
		t.Fatalf("recovered job: %+v", pending[0])
	}
}

func TestIdle(t *testing.T) {
	q, _ := newQueue()
	if idle, _ := q.Idle(); !idle {
		t.Fatal("an empty queue is not idle")
	}
	q.Enqueue("echo", "", time.Hour)
	if idle, _ := q.Idle(); idle {
		t.Fatal("a queue with a delayed job is idle")
	}
}

func TestParseState(t *testing.T) {
	for _, st := range queue.States() {
		got, err := queue.ParseState(" " + strings.ToUpper(string(st)) + " ")
		if err != nil || got != st {
			t.Errorf("ParseState(%q) = %q, %v", st, got, err)
		}
	}
	if _, err := queue.ParseState("lost"); err == nil {
		t.Fatal("ParseState accepted an unknown state")
	}
	if !queue.Done.Terminal() || queue.Pending.Terminal() || queue.Running.Terminal() {
		t.Fatal("Terminal() is wrong")
	}
}

func TestValidate(t *testing.T) {
	if err := queue.NewJob("echo", "x").Validate(); err != nil {
		t.Fatal(err)
	}
	bad := queue.Job{ID: "job-0001", Kind: "a b", State: "nope", Attempts: -1}
	err := bad.Validate()
	if err == nil {
		t.Fatal("Validate accepted a bad job")
	}
	for _, want := range []string{"job-0001", "white space", "unknown state", "negative attempts"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}
