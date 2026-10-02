package queue

import (
	"errors"
	"time"
)

// ErrNotFound is returned by a Store for an ID it does not hold.
var ErrNotFound = errors.New("job not found")

// Store keeps the tasks. The store package has two: an in-memory one and one
// backed by a JSON file. A Store must be safe for concurrent use, since every
// worker of a pool calls it.
type Store interface {
	// Add stores a new task, assigns its ID, and returns it as stored.
	Add(t Task) (Task, error)
	// Get returns the task with the given ID, or ErrNotFound.
	Get(id string) (Task, error)
	// Update replaces the stored task that has t's ID, or returns
	// ErrNotFound.
	Update(t Task) error
	// Delete removes the task with the given ID, or returns ErrNotFound.
	Delete(id string) error
	// Claim picks the oldest task that is ready at now (see Task.Ready),
	// marks it Running, counts the attempt, and returns it. It returns false
	// when no task is ready.
	Claim(now time.Time) (Task, bool, error)
	// List returns the tasks in the given state, oldest first, or every task
	// when state is "".
	List(state State) ([]Task, error)
	// Counts returns the number of tasks in each state, with every state
	// present.
	Counts() (map[State]int, error)
}
