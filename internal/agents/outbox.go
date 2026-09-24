package agents

import "sync"

// outbox delivers a parent's updates in order without making the sender
// wait: a parent whose session is busy then holds up only its own updates,
// never a child's event loop or another parent's updates. A goroutine
// drains it while it has work and ends when it is empty.
type outbox struct {
	mu      sync.Mutex
	queue   []func()
	running bool
}

// push adds a delivery; deliveries run one at a time in push order.
func (o *outbox) push(deliver func()) {
	o.mu.Lock()
	o.queue = append(o.queue, deliver)
	start := !o.running
	o.running = true
	o.mu.Unlock()
	if start {
		go o.drain()
	}
}

func (o *outbox) drain() {
	for {
		o.mu.Lock()
		if len(o.queue) == 0 {
			o.running = false
			o.mu.Unlock()

			return
		}
		deliver := o.queue[0]
		o.queue[0] = nil
		o.queue = o.queue[1:]
		o.mu.Unlock()
		deliver()
	}
}
