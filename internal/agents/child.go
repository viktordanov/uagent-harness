package agents

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/hooks"
	"github.com/viktordanov/uagent-harness/internal/session"
)

// nicknames are given to children of roles without nickname candidates.
var nicknames = []string{
	"Ada", "Babbage", "Curie", "Darwin", "Euler", "Faraday", "Gauss", "Hopper",
	"Hypatia", "Kepler", "Lovelace", "Maxwell", "Noether", "Pascal", "Turing", "Volta",
}

// child is a spawned or resumed session. The manager's lock guards its
// fields.
type child struct {
	id, parent, role, nickname string
	s                          *session.Session
	status                     Status
	// started is when the child's current work began.
	started time.Time
	closed  bool
	// forked is a child started with fork_context.
	forked bool
	// gen counts the messages sent. sending are the ones being submitted,
	// pending the ones submitted that the session has not queued yet, and
	// early the ones it queued before their submit returned, so an Idle from
	// before a message does not end the child's work.
	gen, sending   int
	pending, early map[string]bool
	// last is the last run's result; failed is a run that did not start.
	last   *core.Result
	failed string
	// asks ends the child's open approvals when it is interrupted or
	// closed; cancel ends it and a new one follows.
	asks   context.Context
	cancel context.CancelFunc
	// stopStreak counts SubagentStop hooks that kept the child going.
	stopStreak int
}

func newChild(id, parent, role, nickname string) *child {
	c := &child{
		id: id, parent: parent, role: role, nickname: nickname, started: time.Now(), status: Status{State: engine.AgentRunning},
		pending: map[string]bool{}, early: map[string]bool{},
	}
	c.asks, c.cancel = context.WithCancel(context.Background())

	return c
}

// cancelAsks ends the child's open approvals. It holds m.mu.
func (c *child) cancelAsks() {
	c.cancel()
	c.asks, c.cancel = context.WithCancel(context.Background())
}

// session is the child's session, nil before it has one.
func (c *child) session(m *Manager) *session.Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	return c.s
}

// nickname picks the preferred name, or the first candidate, that no open
// child of the parent has. It holds m.mu.
func (m *Manager) nickname(parentID string, role Role, preferred string) string {
	used := map[string]bool{}
	for _, c := range m.children {
		if c.parent == parentID && !c.closed {
			used[c.nickname] = true
		}
	}
	if preferred != "" && !used[preferred] {
		return preferred
	}
	candidates := role.NicknameCandidates
	if len(candidates) == 0 {
		candidates = nicknames
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

// submit sends a child a message, now or after its live run stops, and
// marks it running. It returns the message's ID.
func (m *Manager) submit(c *child, message string, now bool) (string, error) {
	m.mu.Lock()
	c.gen++
	c.sending++
	if c.status.Final() {
		c.started = time.Now()
	}
	c.status = Status{State: engine.AgentRunning}
	s := c.s
	m.mu.Unlock()
	m.notify(c)
	submit := s.Submit
	if now {
		submit = s.SteerNow
	}
	in, err := submit(message)
	m.mu.Lock()
	c.sending--
	if err == nil && !c.early[in.ID] {
		c.pending[in.ID] = true
	}
	delete(c.early, in.ID)
	m.mu.Unlock()
	if err != nil {
		return "", fmt.Errorf("failed to send the agent the message: %w", err)
	}

	return in.ID, nil
}

// watch follows a child's events until its session closes.
func (m *Manager) watch(c *child) {
	for e := range c.s.Events() {
		m.mu.Lock()
		changed, check := m.observe(c, e)
		m.mu.Unlock()
		if check != nil {
			go m.checkStop(c, *check)
		}
		if changed {
			m.notify(c)
		}
		m.forward(c, e)
	}
	m.mu.Lock()
	c.closed = true
	c.status = Status{State: engine.AgentShutdown}
	m.mu.Unlock()
	m.notify(c)
}

// observe folds one event into the child and reports whether its status
// changed, or the stop check to run before it does. It holds m.mu.
func (m *Manager) observe(c *child, e core.Event) (bool, *stopCheck) {
	switch e := e.(type) {
	case session.InputQueued:
		if c.pending[e.Input.ID] {
			delete(c.pending, e.Input.ID)
		} else {
			c.early[e.Input.ID] = true // or the session's own, such as a Stop hook's
		}
	case session.InputFailed:
		c.failed = e.Reason
	case core.RunFinished:
		r := e.Result
		c.last, c.failed = &r, ""
	case session.Notice:
		if e.Level == session.LevelError {
			c.failed = e.Message
		}
	case session.Idle:
		if c.sending > 0 || len(c.pending) > 0 || c.closed {
			return false, nil
		}
		status := c.final()
		c.last, c.failed = nil, ""
		clear(c.early)
		if status.State == engine.AgentCompleted && m.tmpl.Hooks.Has(hooks.SubagentStop, "") {
			return false, &stopCheck{gen: c.gen, status: status}
		}
		c.status, c.stopStreak = status, 0

		return true, nil
	}

	return false, nil
}

// final is the status an idle child reports. It holds m.mu.
func (c *child) final() Status {
	switch {
	case c.last != nil && c.last.Status == core.StatusOK:
		return Status{State: engine.AgentCompleted, Message: c.last.Answer}
	case c.last != nil && c.last.Status == core.StatusInterrupted:
		return Status{State: engine.AgentInterrupted}
	case c.last != nil:
		msg := strings.TrimSpace(fmt.Sprintf("the run ended with status %s. %s", c.last.Status, c.last.Answer))

		return Status{State: engine.AgentErrored, Message: msg}
	case c.failed != "":
		return Status{State: engine.AgentErrored, Message: c.failed}
	}

	return Status{State: engine.AgentCompleted}
}

// notify wakes the waiters and tells the parent. Updates reach the parent
// in the order their states were taken.
func (m *Manager) notify(c *child) {
	m.emitMu.Lock()
	defer m.emitMu.Unlock()
	m.mu.Lock()
	close(m.changed)
	m.changed = make(chan struct{})
	current := m.children[c.id] == c // not a closed child a resume replaced
	emit := m.parents[c.parent].Emit
	update := engine.AgentUpdated{At: time.Now(), ID: c.id, Nickname: c.nickname, Role: c.role, State: c.status.State, Started: c.started}
	m.mu.Unlock()
	if emit != nil && current {
		emit(update)
	}
}

// forward passes a child's tool events to the parent, for its detailed
// view.
func (m *Manager) forward(c *child, e core.Event) {
	switch e.(type) {
	case core.ToolCalled, core.ToolStarted, core.ToolFinished:
	default:
		return
	}
	m.emitMu.Lock()
	defer m.emitMu.Unlock()
	m.mu.Lock()
	emit := m.parents[c.parent].Emit
	m.mu.Unlock()
	if emit != nil {
		emit(engine.AgentActivity{At: e.OccurredAt(), ID: c.id, Event: e})
	}
}
