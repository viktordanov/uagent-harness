package agents

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/session"
)

// spawn starts a child with its first message and returns at once.
func (m *Manager) spawn(ctx context.Context, parentID string, a spawnArgs) (spawnResult, error) {
	role, err := m.role(a.AgentType)
	if err != nil {
		return spawnResult{}, err
	}
	rec := record{Role: role.Name, Model: a.Model, Effort: a.Effort}
	c, err := m.start(parentID, session.NewSubagentID(), role, rec, false) //nolint:contextcheck // children outlive the call that started them
	if err != nil {
		return spawnResult{}, err
	}
	if _, err := m.submit(c, a.Message, false); err != nil {
		m.closeTree(c)

		return spawnResult{}, err
	}
	if ctx.Err() != nil { // the parent's run stopped meanwhile
		m.closeTree(c)

		return spawnResult{}, fmt.Errorf("the spawn stopped: %w", ctx.Err())
	}

	return spawnResult{AgentID: c.id, Nickname: c.nickname}, nil
}

// start registers a child within the limit, opens its session, and
// watches it. A resumed child opens its earlier session.
func (m *Manager) start(parentID, id string, role Role, rec record, resumed bool) (*child, error) {
	m.mu.Lock()
	parent, ok := m.parents[parentID]
	if !ok || m.eng == nil {
		m.mu.Unlock()

		return nil, errors.New("subagents are not available in this session")
	}
	if old, ok := m.children[id]; ok && !old.closed { // resumed twice at once
		m.mu.Unlock()

		return old, nil
	}
	if open := m.openIn(m.root(parentID)); open >= m.cfg.MaxThreads {
		m.mu.Unlock()

		return nil, fmt.Errorf("agent limit reached: %d agents are open; close one with close_agent first", open)
	}
	c := newChild(id, parentID, role.Name, m.nickname(parentID, role, rec.Nickname))
	if resumed {
		c.status = Status{State: engine.AgentPendingInit}
	}
	m.children[id] = c
	eng, opts := m.eng, m.childOptions(parent, c, role, rec, resumed)
	m.mu.Unlock()

	// Children outlive the call that started them; Close stops them.
	s, err := session.Open(context.Background(), eng, opts) //nolint:contextcheck // children outlive the spawning call
	if err != nil {
		m.mu.Lock()
		delete(m.children, id)
		m.mu.Unlock()

		return nil, fmt.Errorf("failed to start the agent: %w", err)
	}
	if !m.adopt(c, s) {
		_ = s.Close()

		return nil, errors.New("the agent was closed before it started")
	}
	rec.Nickname = c.nickname
	_ = writeRecord(opts.SessionsDir, id, rec) // only resume_agent's nickname and role depend on it
	go m.watch(c)
	m.notify(c)

	return c, nil
}

// adopt gives a child its session unless it was closed meanwhile.
func (m *Manager) adopt(c *child, s *session.Session) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c.closed {
		return false
	}
	c.s = s

	return true
}

// find returns the parent's child by ID, closed or not. It holds m.mu.
func (m *Manager) find(parentID, id string) (*child, bool) {
	c, ok := m.children[id]
	if !ok || c.parent != parentID || c.s == nil {
		return nil, false
	}

	return c, true
}

// notFound says a child is unknown, and how to reach one from an earlier
// process.
func (m *Manager) notFound(parentID, id string) error {
	if sc, found, err := session.ReadSidecar(m.template().SessionsDir, id); id != "" && err == nil && found && sc.Parent == parentID {
		return fmt.Errorf("agent with id %s is not loaded; resume it with resume_agent first", id)
	}

	return fmt.Errorf("agent with id %s not found", id)
}

// send gives a child another message; with interrupt, it stops the
// child's live run and handles the message at once.
func (m *Manager) send(parentID, id, message string, interrupt bool) (string, error) {
	m.mu.Lock()
	c, ok := m.find(parentID, id)
	closed := ok && c.closed
	if ok && interrupt {
		c.cancelAsks()
	}
	if ok {
		c.stopStreak = 0
	}
	m.mu.Unlock()
	switch {
	case !ok:
		return "", m.notFound(parentID, id)
	case closed:
		return "", fmt.Errorf("agent with id %s is closed", id)
	}
	if interrupt {
		if err := c.s.Interrupt(); err != nil {
			return "", fmt.Errorf("failed to interrupt the agent: %w", err)
		}
	}

	return m.submit(c, message, interrupt)
}

// wait returns when any of the children reaches a final status, with every
// final one's status, or when the timeout passes. An unknown child is
// final as not_found.
func (m *Manager) wait(ctx context.Context, parentID string, ids []string, timeout time.Duration) (map[string]Status, bool, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		m.mu.Lock()
		out := map[string]Status{}
		for _, id := range ids {
			c, ok := m.find(parentID, id)
			switch {
			case !ok:
				out[id] = Status{State: engine.AgentNotFound}
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
			return map[string]Status{}, true, nil
		case <-ctx.Done():
			return nil, false, fmt.Errorf("the wait stopped: %w", ctx.Err())
		}
	}
}

// closeAgent closes a child and its descendants and returns its status
// before it closed.
func (m *Manager) closeAgent(parentID, id string) (Status, error) {
	m.mu.Lock()
	c, ok := m.find(parentID, id)
	var prev Status
	if ok {
		prev = c.status
	}
	m.mu.Unlock()
	if !ok {
		return Status{State: engine.AgentNotFound}, m.notFound(parentID, id)
	}
	m.closeTree(c)

	return prev, nil
}

// closeTree closes a child and its open descendants, which stop counting
// toward the limit at once; their watchers mark them shut down.
func (m *Manager) closeTree(c *child) {
	m.mu.Lock()
	tree := m.subtree(c)
	for _, k := range tree {
		k.closed = true
		k.cancelAsks()
	}
	m.mu.Unlock()
	for _, k := range tree {
		if s := k.session(m); s != nil {
			_ = s.Close()
		}
	}
}

// resume opens a closed child again, or one from an earlier process, as
// long as its sidecar names this parent; an open child reports its status.
func (m *Manager) resume(_ context.Context, parentID, id string) (Status, error) {
	m.mu.Lock()
	c, ok := m.children[id]
	if ok && c.parent == parentID && !c.closed {
		status := c.status
		m.mu.Unlock()

		return status, nil
	}
	m.mu.Unlock()
	dir := m.template().SessionsDir
	sc, found, err := session.ReadSidecar(dir, id)
	if id == "" || err != nil || !found || sc.Source != session.SourceSubagent || sc.Parent != parentID {
		return Status{State: engine.AgentNotFound}, fmt.Errorf("agent with id %s not found", id)
	}
	rec, _ := readRecord(dir, id)
	role, err := m.role(rec.Role)
	if err != nil {
		role = Role{} // the role file is gone: the default agent with the child's model
	}
	c, err = m.start(parentID, id, role, rec, true) //nolint:contextcheck // children outlive the call that started them
	if err != nil {
		return Status{State: engine.AgentNotFound}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	return c.status, nil
}
