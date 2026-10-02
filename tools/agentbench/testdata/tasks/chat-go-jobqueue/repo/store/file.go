package store

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"example.com/jobq/queue"
)

// File is a queue.Store backed by a JSON file. It keeps every task in a
// Memory and rewrites the whole file after each change, through a temporary
// file and a rename, so a crash leaves either the old file or the new one.
//
// Only one process should use a file at a time; File does not lock it.
type File struct {
	path string
	mem  *Memory
	mu   sync.Mutex // serializes writes to path
}

// OpenFile opens the store kept in path. A missing file is an empty store; it
// is created on the first change.
func OpenFile(path string) (*File, error) {
	f := &File{path: path, mem: NewMemory()}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	next, tasks, err := decode(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := f.mem.restore(next, tasks); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return f, nil
}

// Path returns the file's path.
func (f *File) Path() string { return f.path }

// Add stores t and saves the file.
func (f *File) Add(t queue.Task) (queue.Task, error) {
	t, err := f.mem.Add(t)
	if err != nil {
		return queue.Task{}, err
	}
	return t, f.save()
}

// Get returns the task with the given ID.
func (f *File) Get(id string) (queue.Task, error) { return f.mem.Get(id) }

// Update replaces a task and saves the file.
func (f *File) Update(t queue.Task) error {
	if err := f.mem.Update(t); err != nil {
		return err
	}
	return f.save()
}

// Delete removes a task and saves the file.
func (f *File) Delete(id string) error {
	if err := f.mem.Delete(id); err != nil {
		return err
	}
	return f.save()
}

// Claim claims a ready task, as Memory.Claim does, and saves the file.
func (f *File) Claim(now time.Time) (queue.Task, bool, error) {
	t, ok, err := f.mem.Claim(now)
	if err != nil || !ok {
		return t, ok, err
	}
	return t, true, f.save()
}

// List returns the tasks in a state, or all of them for "".
func (f *File) List(state queue.State) ([]queue.Task, error) { return f.mem.List(state) }

// Counts returns the number of tasks in each state.
func (f *File) Counts() (map[queue.State]int, error) { return f.mem.Counts() }

// save writes the current contents to the file. The snapshot is taken under
// f.mu, so of two concurrent saves the later one writes the later state.
func (f *File) save() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	next, tasks := f.mem.snapshot()
	var buf bytes.Buffer
	if err := encode(&buf, next, tasks); err != nil {
		return err
	}
	if err := writeAtomic(f.path, buf.Bytes()); err != nil {
		return fmt.Errorf("save store: %w", err)
	}
	return nil
}

// writeAtomic writes b to a temporary file next to path and renames it over
// path.
func writeAtomic(path string, b []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
