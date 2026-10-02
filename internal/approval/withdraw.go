package approval

import (
	"context"
	"errors"
	"sync"
)

// ErrNowAllowed ends an ask that a rule or setting added while it waited
// settles: "don't ask again" on another prompt open at the same time. The
// session resolves such a prompt as approved.
var ErrNowAllowed = errors.New("a rule added while the prompt waited allows it")

// Changes tells waiting asks that what settles them may have changed, such
// as a new allow rule. The zero value is ready, a nil one never changes,
// and it is safe for concurrent use.
type Changes struct {
	mu sync.Mutex
	ch chan struct{}
}

// next is closed at the next change.
func (c *Changes) next() <-chan struct{} {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ch == nil {
		c.ch = make(chan struct{})
	}

	return c.ch
}

// Notify wakes the asks waiting for a change.
func (c *Changes) Notify() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ch != nil {
		close(c.ch)
		c.ch = nil
	}
}

// AskUnless asks, and withdraws the prompt once settled reports true after
// a change: the ask's context then ends with ErrNowAllowed, and AskUnless
// reports settled without an answer. The calls of one model response are
// decided at once, so several prompts can be open together, and a "don't
// ask again" on one settles the others it covers, as it would if they were
// asked one after another.
func AskUnless(ctx context.Context, ask Ask, p Prompt, changes *Changes, settled func() bool) (answer Answer, done bool) {
	if changes == nil {
		return ask(ctx, p), false
	}
	next := changes.next()
	if settled() {
		return "", true
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	answers := make(chan Answer, 1)
	go func() { answers <- ask(ctx, p) }()
	for {
		select {
		case a := <-answers:
			return a, false
		case <-next:
			next = changes.next()
			if settled() {
				cancel(ErrNowAllowed)
				<-answers // an ask returns once its context ends

				return "", true
			}
		}
	}
}
