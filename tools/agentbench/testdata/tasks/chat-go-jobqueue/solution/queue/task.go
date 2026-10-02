// Package queue holds the job model and the queue that moves jobs between
// states on top of a Store.
package queue

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Job is one job in the queue: a kind, which picks the handler that runs it,
// a payload the handler reads, and the bookkeeping of its attempts.
type Job struct {
	// ID is assigned by the store when the task is added.
	ID string `json:"id"`
	// Kind names the handler that runs the task, such as "echo".
	Kind string `json:"kind"`
	// Payload is the handler's input, free-form text.
	Payload string `json:"payload,omitempty"`
	// State is where the task is in its life.
	State State `json:"state"`
	// Attempts counts the times a worker has claimed the task.
	Attempts int `json:"attempts"`
	// LastError is the error of the last failed attempt.
	LastError string `json:"last_error,omitempty"`
	// CreatedAt is when the task was enqueued.
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt is when the task last changed state.
	UpdatedAt time.Time `json:"updated_at"`
	// RunAt is the earliest time a worker may claim the task. A retried task
	// gets a RunAt in the future; the zero time means at once.
	RunAt time.Time `json:"run_at"`
}

// NewJob returns a pending task of the given kind and payload, with no ID
// yet.
func NewJob(kind, payload string) Job {
	return Job{Kind: kind, Payload: payload, State: Pending}
}

// Validate reports what is wrong with t, if anything.
func (t Job) Validate() error {
	var errs []error
	switch {
	case t.Kind == "":
		errs = append(errs, errors.New("empty kind"))
	case strings.ContainsAny(t.Kind, " \t\r\n"):
		errs = append(errs, fmt.Errorf("kind %q has white space", t.Kind))
	}
	if !t.State.Valid() {
		errs = append(errs, fmt.Errorf("unknown state %q", string(t.State)))
	}
	if t.Attempts < 0 {
		errs = append(errs, fmt.Errorf("negative attempts %d", t.Attempts))
	}
	if err := errors.Join(errs...); err != nil {
		if t.ID != "" {
			return fmt.Errorf("job %s: %w", t.ID, err)
		}
		return fmt.Errorf("job: %w", err)
	}
	return nil
}

// Ready reports whether a worker may claim t at now: it is pending and its
// RunAt has come.
func (t Job) Ready(now time.Time) bool {
	return t.State == Pending && !t.RunAt.After(now)
}

// String describes t in one line for logs.
func (t Job) String() string {
	id := t.ID
	if id == "" {
		id = "(new)"
	}
	return fmt.Sprintf("%s %s [%s, %d attempts]", id, t.Kind, t.State, t.Attempts)
}
