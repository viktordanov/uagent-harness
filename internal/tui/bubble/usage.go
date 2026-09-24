package bubble

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/viktordanov/uagent-harness/internal/tui/state"
	"github.com/viktordanov/uagent-harness/internal/usage"
)

// loadUsage reads the subscription's usage off the update loop. Without a
// reader the TUI has no usage, as for a provider without one.
func (m Model) loadUsage(e state.EffLoadUsage) tea.Cmd {
	reader, now, ctx := m.deps.Usage, m.deps.Now, m.ctx

	return func() tea.Msg {
		if reader == nil {
			return state.UsageLoaded{Reason: e.Reason, Err: fmt.Errorf("%w here", usage.ErrUnsupported), At: now()}
		}
		s, err := reader.Usage(ctx, e.MaxAge)

		return state.UsageLoaded{Reason: e.Reason, Snapshot: s, Err: err, At: now()}
	}
}
