package render

import (
	"github.com/charmbracelet/x/ansi"

	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// agentScreen draws a subagent's transcript, as the session's own is
// drawn, under one header line. The agent's items have a cache of their
// own, since their keys are the agent's.
func agentScreen(s state.State, c *Cache, f Frame) (string, int) {
	v := s.View
	if c.view == nil || c.viewID != v.ID {
		c.view, c.viewID = NewCache(), v.ID
	}
	f.Height = max(f.Height-1, 1)
	out, row := Screen(*v.St, c.view, f)
	c.maxScroll = c.view.maxScroll
	header := " " + Accent().Render("agent "+v.Nickname) + Dim().Render(" · alt+← alt+→ switch agents · esc esc interrupts")

	return ansi.Truncate(header, f.Width, "") + "\n" + out, row + 1
}
