package bubble

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/viktordanov/uah/internal/tui/state"
	"github.com/viktordanov/uah/internal/usage"
	"github.com/viktordanov/uah/internal/usage/cachestats"
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

// loadCache reads the session's prompt cache accounting off the update
// loop. Without a reader the TUI shows none.
func (m Model) loadCache(e state.EffLoadCache) tea.Cmd {
	read := m.deps.Cache
	if read == nil {
		return nil
	}

	return func() tea.Msg {
		reqs, err := read(e.SessionID)

		return state.CacheLoaded{Summary: cachestats.Summarize(reqs, cachestats.APIPrice), Err: err}
	}
}
