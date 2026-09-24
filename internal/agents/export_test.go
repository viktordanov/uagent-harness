package agents

import (
	"context"
	"time"
)

// Wait is wait_agent without its 10-second minimum, for tests.
func (m *Manager) Wait(ctx context.Context, parentID string, ids []string, timeout time.Duration) (map[string]Status, bool, error) {
	return m.wait(ctx, parentID, ids, timeout)
}
