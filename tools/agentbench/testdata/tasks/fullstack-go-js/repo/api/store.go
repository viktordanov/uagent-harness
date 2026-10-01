// Package api serves the task tracker's HTTP API.
package api

import (
	"sort"
	"sync"
	"time"
)

// Statuses a task moves through, in order.
const (
	StatusTodo  = "todo"
	StatusDoing = "doing"
	StatusDone  = "done"
)

// Task is one task.
type Task struct {
	ID      int       `json:"id"`
	Title   string    `json:"title"`
	Status  string    `json:"status"`
	Created time.Time `json:"created"`
}

// Store keeps tasks in memory.
type Store struct {
	mu    sync.Mutex
	next  int
	tasks []Task
	now   func() time.Time
}

// NewStore returns an empty store; now gives creation times.
func NewStore(now func() time.Time) *Store {
	return &Store{next: 1, now: now}
}

// Add creates a task in the todo status.
func (s *Store) Add(title string) Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := Task{ID: s.next, Title: title, Status: StatusTodo, Created: s.now()}
	s.next++
	s.tasks = append(s.tasks, t)

	return t
}

// SetStatus moves a task to status; it reports whether the task exists.
func (s *Store) SetStatus(id int, status string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			s.tasks[i].Status = status

			return true
		}
	}

	return false
}

// List returns every task, newest first.
func (s *Store) List() []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]Task(nil), s.tasks...)
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })

	return out
}
