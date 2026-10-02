// Package store holds the two queue.Store implementations: Memory, which
// keeps the tasks in a map, and File, which keeps a Memory and writes it to a
// JSON file after every change.
package store

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"example.com/jobq/queue"
)

// idPrefix starts every task ID; the rest is a decimal sequence number.
const idPrefix = "job-"

// Memory is a queue.Store in memory. The zero value is not usable; call
// NewMemory.
type Memory struct {
	mu    sync.RWMutex
	tasks map[string]queue.Task
	order []string // IDs, oldest first
	next  int      // the sequence number of the last ID handed out
}

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{tasks: make(map[string]queue.Task)}
}

// Add stores t under a new ID and returns it as stored. A task with no state
// is stored Pending.
func (m *Memory) Add(t queue.Task) (queue.Task, error) {
	if t.State == "" {
		t.State = queue.Pending
	}
	if err := t.Validate(); err != nil {
		return queue.Task{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.next++
	t.ID = formatID(m.next)
	m.tasks[t.ID] = t
	m.order = append(m.order, t.ID)
	return t, nil
}

// Get returns the task with the given ID.
func (m *Memory) Get(id string) (queue.Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tasks[id]
	if !ok {
		return queue.Task{}, fmt.Errorf("%s: %w", id, queue.ErrNotFound)
	}
	return t, nil
}

// Update replaces the stored task with t's ID.
func (m *Memory) Update(t queue.Task) error {
	if err := t.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tasks[t.ID]; !ok {
		return fmt.Errorf("%s: %w", t.ID, queue.ErrNotFound)
	}
	m.tasks[t.ID] = t
	return nil
}

// Delete removes the task with the given ID.
func (m *Memory) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tasks[id]; !ok {
		return fmt.Errorf("%s: %w", id, queue.ErrNotFound)
	}
	delete(m.tasks, id)
	for i, oid := range m.order {
		if oid == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	return nil
}

// Claim marks the oldest task that is ready at now Running, counts the
// attempt, and returns it.
func (m *Memory) Claim(now time.Time) (queue.Task, bool, error) {
	id, ok := m.nextReady(now)
	if !ok {
		return queue.Task{}, false, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.tasks[id]
	t.State = queue.Running
	t.Attempts++
	t.UpdatedAt = now
	m.tasks[id] = t
	return t, true, nil
}

// nextReady returns the ID of the oldest task that is ready at now.
func (m *Memory) nextReady(now time.Time) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, id := range m.order {
		if m.tasks[id].Ready(now) {
			return id, true
		}
	}
	return "", false
}

// List returns the tasks in the given state, oldest first, or all of them
// for "".
func (m *Memory) List(state queue.State) ([]queue.Task, error) {
	if state != "" && !state.Valid() {
		return nil, fmt.Errorf("list: unknown state %q", string(state))
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []queue.Task
	for _, id := range m.order {
		t := m.tasks[id]
		if state == "" || t.State == state {
			out = append(out, t)
		}
	}
	return out, nil
}

// Counts returns the number of tasks in each state, every state present.
func (m *Memory) Counts() (map[queue.State]int, error) {
	counts := make(map[queue.State]int)
	for _, st := range queue.States() {
		counts[st] = 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, t := range m.tasks {
		counts[t.State]++
	}
	return counts, nil
}

// Len returns the number of tasks.
func (m *Memory) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.tasks)
}

// snapshot returns the sequence number and a copy of every task, oldest
// first, for File to save.
func (m *Memory) snapshot() (int, []queue.Task) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]queue.Task, 0, len(m.order))
	for _, id := range m.order {
		out = append(out, m.tasks[id])
	}
	return m.next, out
}

// restore replaces the contents with tasks, in the given order, and sets the
// sequence number to at least next and past every ID it holds.
func (m *Memory) restore(next int, tasks []queue.Task) error {
	byID := make(map[string]queue.Task, len(tasks))
	order := make([]string, 0, len(tasks))
	for _, t := range tasks {
		if err := t.Validate(); err != nil {
			return err
		}
		if t.ID == "" {
			return fmt.Errorf("a job has no id")
		}
		if _, dup := byID[t.ID]; dup {
			return fmt.Errorf("job %s appears twice", t.ID)
		}
		byID[t.ID] = t
		order = append(order, t.ID)
		if n, ok := parseID(t.ID); ok && n > next {
			next = n
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks = byID
	m.order = order
	m.next = next
	return nil
}

func formatID(n int) string {
	return fmt.Sprintf("%s%04d", idPrefix, n)
}

func parseID(id string) (int, bool) {
	rest, ok := strings.CutPrefix(id, idPrefix)
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}
