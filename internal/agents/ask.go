package agents

import (
	"context"
	"strings"

	"github.com/viktordanov/uagent-harness/internal/approval"
)

// askFor asks a child's approvals through its parent's session, with the
// child's nickname in the justification. The prompt stays open after the
// parent's run ends; it ends when the child is interrupted or closed.
func (m *Manager) askFor(c *child) approval.Ask {
	return func(ctx context.Context, p approval.Prompt) approval.Answer {
		m.mu.Lock()
		ask := m.parents[c.parent].Ask
		asks := c.asks
		m.mu.Unlock()
		if ask == nil {
			return approval.DeclineBecause("no one can approve commands of agent " + c.nickname + " in this session")
		}
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		stop := context.AfterFunc(asks, cancel) //nolint:contextcheck // the child's interrupt or close also ends the prompt
		defer stop()
		p.Justification = strings.TrimSpace("agent " + c.nickname + ": " + p.Justification)

		return ask(ctx, p)
	}
}
