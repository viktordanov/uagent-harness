package queue

import (
	"fmt"
	"time"

	"example.com/jobq/internal/clock"
)

// Queue moves tasks between states on top of a Store. It stamps every change
// with the time of its clock and refuses a change that does not start from
// the state it expects, so a worker can only finish a task it is running.
type Queue struct {
	store Store
	clock clock.Clock
}

// New returns a queue over s that reads the time from c, or from the wall
// clock when c is nil.
func New(s Store, c clock.Clock) *Queue {
	if c == nil {
		c = clock.System()
	}
	return &Queue{store: s, clock: c}
}

// Store returns the queue's store.
func (q *Queue) Store() Store { return q.store }

// Clock returns the queue's clock.
func (q *Queue) Clock() clock.Clock { return q.clock }

// Enqueue adds a pending task of the given kind and payload that becomes
// ready after delay (at once when delay is zero or less).
func (q *Queue) Enqueue(kind, payload string, delay time.Duration) (Task, error) {
	now := q.clock.Now()
	t := NewTask(kind, payload)
	t.CreatedAt = now
	t.UpdatedAt = now
	t.RunAt = now
	if delay > 0 {
		t.RunAt = now.Add(delay)
	}
	if err := t.Validate(); err != nil {
		return Task{}, err
	}
	return q.store.Add(t)
}

// Claim takes the oldest ready task for a worker, which then runs it and
// calls Complete, Retry or Fail. It returns false when no task is ready.
func (q *Queue) Claim() (Task, bool, error) {
	return q.store.Claim(q.clock.Now())
}

// Complete marks a running task Done.
func (q *Queue) Complete(t Task) error {
	return q.transition(t.ID, Running, func(cur *Task) {
		cur.State = Done
		cur.LastError = ""
	})
}

// Retry puts a running task whose attempt failed with cause back to Pending,
// ready again after delay.
func (q *Queue) Retry(t Task, cause error, delay time.Duration) error {
	return q.transition(t.ID, Running, func(cur *Task) {
		cur.State = Pending
		cur.LastError = errText(cause)
		cur.RunAt = q.clock.Now()
		if delay > 0 {
			cur.RunAt = cur.RunAt.Add(delay)
		}
	})
}

// Fail marks a running task Failed: it has no attempt left.
func (q *Queue) Fail(t Task, cause error) error {
	return q.transition(t.ID, Running, func(cur *Task) {
		cur.State = Failed
		cur.LastError = errText(cause)
	})
}

// Requeue puts a failed task back to Pending with its attempts reset, ready
// at once, and returns it.
func (q *Queue) Requeue(id string) (Task, error) {
	var out Task
	err := q.transition(id, Failed, func(cur *Task) {
		cur.State = Pending
		cur.Attempts = 0
		cur.RunAt = q.clock.Now()
		out = *cur
	})
	return out, err
}

// RecoverRunning puts every Running task back to Pending, ready at once, and
// returns how many it moved. A task is left Running only when the process that
// ran it stopped before finishing it, so it is called before a pool starts.
// The interrupted attempt still counts.
func (q *Queue) RecoverRunning() (int, error) {
	running, err := q.store.List(Running)
	if err != nil {
		return 0, err
	}
	for _, t := range running {
		if err := q.transition(t.ID, Running, func(cur *Task) {
			cur.State = Pending
			cur.RunAt = q.clock.Now()
			if cur.LastError == "" {
				cur.LastError = "interrupted"
			}
		}); err != nil {
			return 0, err
		}
	}
	return len(running), nil
}

// Idle reports whether the queue has nothing left to do: no task pending
// (ready or waiting for a retry) and none running.
func (q *Queue) Idle() (bool, error) {
	counts, err := q.store.Counts()
	if err != nil {
		return false, err
	}
	return counts[Pending]+counts[Running] == 0, nil
}

// Get returns the task with the given ID.
func (q *Queue) Get(id string) (Task, error) { return q.store.Get(id) }

// List returns the tasks in the given state, or every task for "".
func (q *Queue) List(state State) ([]Task, error) { return q.store.List(state) }

// transition loads task id, checks that it is in state from, applies change,
// stamps UpdatedAt, and stores it.
func (q *Queue) transition(id string, from State, change func(*Task)) error {
	cur, err := q.store.Get(id)
	if err != nil {
		return fmt.Errorf("queue: %s: %w", id, err)
	}
	if cur.State != from {
		return fmt.Errorf("queue: job %s is %s, not %s", id, cur.State, from)
	}
	change(&cur)
	cur.UpdatedAt = q.clock.Now()
	if err := q.store.Update(cur); err != nil {
		return fmt.Errorf("queue: %s: %w", id, err)
	}
	return nil
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
