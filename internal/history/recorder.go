package history

import "sync"

// Recorder appends entries to a file in the order Add saw them, from any
// goroutine: the TUI adds on its update loop and flushes off it, and two
// flushes that race write every entry once, in order.
type Recorder struct {
	file    File
	mu      sync.Mutex
	pending []Entry
}

// NewRecorder returns a recorder for file.
func NewRecorder(file File) *Recorder { return &Recorder{file: file} }

// File is the file the recorder writes.
func (r *Recorder) File() File { return r.file }

// Add queues an entry for the next Flush. It does no I/O.
func (r *Recorder) Add(e Entry) {
	r.mu.Lock()
	r.pending = append(r.pending, e)
	r.mu.Unlock()
}

// Flush appends the queued entries. Entries that fail to append are
// dropped, as Codex drops a prompt it could not write.
func (r *Recorder) Flush() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	pending := r.pending
	r.pending = nil

	return r.file.Append(pending...)
}
