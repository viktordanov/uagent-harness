package queue

import "sync"

type deadLetter struct {
	Job    Job
	Reason string
}

// deadLetters keeps jobs that will not be retried, for an operator.
type deadLetters struct {
	mu    sync.Mutex
	items []deadLetter
}

func (d *deadLetters) add(j Job, reason string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.items = append(d.items, deadLetter{j, reason})
}

// DeadLetters returns the jobs given up on.
func (d *Dispatcher) DeadLetters() []Job {
	d.dead.mu.Lock()
	defer d.dead.mu.Unlock()
	out := make([]Job, len(d.dead.items))
	for i, x := range d.dead.items {
		out[i] = x.Job
	}

	return out
}
