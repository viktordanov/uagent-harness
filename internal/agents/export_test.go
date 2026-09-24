package agents

import (
	"context"
	"time"

	"github.com/viktordanov/uagent-harness/internal/session"
)

// Wait is wait_agent without its 10-second minimum, for tests.
func (m *Manager) Wait(ctx context.Context, parentID string, ids []string, timeout time.Duration) (map[string]Status, bool, error) {
	return m.wait(ctx, parentID, ids, timeout)
}

// ChildOptions are the session options a default child of the parent's
// latest run would open with.
func (m *Manager) ChildOptions(parentID string) session.Options {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.childOptions(m.parents[parentID], newChild("child-id", parentID, "", "Ada"), Role{}, record{}, false)
}
