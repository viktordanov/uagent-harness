package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/session"
)

// nicknames are given to children of roles without nickname candidates.
var nicknames = []string{
	"Ada", "Babbage", "Curie", "Darwin", "Euler", "Faraday", "Gauss", "Hopper",
	"Hypatia", "Kepler", "Lovelace", "Maxwell", "Noether", "Pascal", "Turing", "Volta",
}

// child is a spawned session. The manager's lock guards its fields.
type child struct {
	id, parent, role, nickname string
	s                          *session.Session
	status                     engine.AgentStatus
	// started is when the child's current work began.
	started time.Time
	closed  bool
	// inputs counts the messages sent and queued the ones the session has
	// accepted, so an Idle from before a message does not end its work.
	inputs, queued int
	// last is the last run's result; failed is a run that did not start.
	last   *core.Result
	failed string
}

// nickname picks the first candidate no open child of the parent has. It
// holds m.mu.
func (m *Manager) nickname(parentID string, role Role) string {
	candidates := role.NicknameCandidates
	if len(candidates) == 0 {
		candidates = nicknames
	}
	used := map[string]bool{}
	for _, c := range m.children {
		if c.parent == parentID && !c.closed {
			used[c.nickname] = true
		}
	}
	for round := 1; ; round++ {
		for _, n := range candidates {
			if round > 1 {
				n = fmt.Sprintf("%s %d", n, round)
			}
			if !used[n] {
				return n
			}
		}
	}
}

// submit sends a child a message and marks it running.
func (m *Manager) submit(c *child, message string) error {
	m.mu.Lock()
	c.inputs++
	if c.status.Final() {
		c.started = time.Now()
	}
	c.status = engine.AgentStatus{State: engine.AgentRunning}
	m.mu.Unlock()
	if _, err := c.s.Submit(message); err != nil {
		return fmt.Errorf("failed to send the agent the message: %w", err)
	}
	m.notify(c)

	return nil
}

// watch follows a child's events until its session closes.
func (m *Manager) watch(c *child) {
	for e := range c.s.Events() {
		m.mu.Lock()
		changed := m.observe(c, e)
		m.mu.Unlock()
		if changed {
			m.notify(c)
		}
	}
	m.mu.Lock()
	c.closed = true
	c.status = engine.AgentStatus{State: engine.AgentShutdown}
	m.mu.Unlock()
	m.notify(c)
}

// observe folds one event into the child and reports whether its status
// changed. It holds m.mu.
func (m *Manager) observe(c *child, e core.Event) bool {
	switch e := e.(type) {
	case session.InputQueued:
		c.queued++
	case core.RunFinished:
		r := e.Result
		c.last, c.failed = &r, ""
	case session.Notice:
		if e.Level == session.LevelError {
			c.failed = e.Message
		}
	case session.Idle:
		if c.queued < c.inputs || c.closed {
			return false
		}
		c.status = c.final()
		c.last, c.failed = nil, ""

		return true
	}

	return false
}

// final is the status an idle child reports. It holds m.mu.
func (c *child) final() engine.AgentStatus {
	switch {
	case c.last != nil && c.last.Status == core.StatusOK:
		return engine.AgentStatus{State: engine.AgentCompleted, Message: c.last.Answer}
	case c.last != nil:
		msg := strings.TrimSpace(fmt.Sprintf("the run ended with status %s. %s", c.last.Status, c.last.Answer))

		return engine.AgentStatus{State: engine.AgentErrored, Message: msg}
	case c.failed != "":
		return engine.AgentStatus{State: engine.AgentErrored, Message: c.failed}
	}

	return engine.AgentStatus{State: engine.AgentCompleted}
}

// notify wakes the waiters and tells the parent's run.
func (m *Manager) notify(c *child) {
	m.mu.Lock()
	close(m.changed)
	m.changed = make(chan struct{})
	emit := m.parents[c.parent].Emit
	update := engine.AgentUpdated{At: time.Now(), ID: c.id, Nickname: c.nickname, Role: c.role, State: c.status.State, Started: c.started}
	m.mu.Unlock()
	if emit != nil {
		emit(update)
	}
}

// find returns the parent's child by ID. It holds m.mu.
func (m *Manager) find(parentID, id string) (*child, bool) {
	c, ok := m.children[id]
	if !ok || c.parent != parentID || c.s == nil {
		return nil, false
	}

	return c, true
}

func (m *Manager) Send(parentID, id, message string) error {
	m.mu.Lock()
	c, ok := m.find(parentID, id)
	closed := ok && c.closed
	m.mu.Unlock()
	switch {
	case !ok:
		return fmt.Errorf("no agent %q (see spawn_agent's id)", id)
	case closed:
		return fmt.Errorf("agent %q is closed", id)
	}

	return m.submit(c, message)
}

func (m *Manager) Wait(ctx context.Context, parentID string, ids []string, timeout time.Duration) (map[string]engine.AgentStatus, bool, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		m.mu.Lock()
		out := map[string]engine.AgentStatus{}
		for _, id := range ids {
			c, ok := m.find(parentID, id)
			switch {
			case !ok:
				out[id] = engine.AgentStatus{State: engine.AgentNotFound}
			case c.status.Final():
				out[id] = c.status
			}
		}
		changed := m.changed
		m.mu.Unlock()
		if len(out) > 0 {
			return out, false, nil
		}
		select {
		case <-changed:
		case <-timer.C:
			return map[string]engine.AgentStatus{}, true, nil
		case <-ctx.Done():
			return nil, false, fmt.Errorf("the wait stopped: %w", ctx.Err())
		}
	}
}

func (m *Manager) CloseAgent(parentID, id string) (engine.AgentStatus, error) {
	m.mu.Lock()
	c, ok := m.find(parentID, id)
	var prev engine.AgentStatus
	if ok {
		prev = c.status
	}
	m.mu.Unlock()
	if !ok {
		return engine.AgentStatus{State: engine.AgentNotFound}, nil
	}
	m.closeTree(c)

	return prev, nil
}

// closeTree closes a child's own children, then the child, which stops
// counting toward the limit at once; its watcher marks it shut down.
func (m *Manager) closeTree(c *child) {
	m.mu.Lock()
	c.closed = true
	var kids []*child
	for _, k := range m.children {
		if k.parent == c.id && k.s != nil {
			kids = append(kids, k)
		}
	}
	m.mu.Unlock()
	for _, k := range kids {
		m.closeTree(k)
	}
	_ = c.s.Close()
}

// Close closes every child; the engine calls it when a session on it
// closes.
func (m *Manager) Close() error {
	m.mu.Lock()
	var all []*child
	for _, c := range m.children {
		if c.s != nil {
			all = append(all, c)
		}
	}
	m.mu.Unlock()
	var errs []error
	for _, c := range all {
		if err := c.s.Close(); err != nil && !errors.Is(err, session.ErrClosed) {
			errs = append(errs, fmt.Errorf("failed to close agent %s: %w", c.nickname, err))
		}
	}

	return errors.Join(errs...)
}
