package render

import (
	"slices"

	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// transcriptLines are an item's lines, with the message selected to go
// back to (state.Backtrack) marked, as Codex highlights it.
func (c *Cache) transcriptLines(s state.State, i, w int) []string {
	it := s.Items[i]
	if s.Backtrack != nil && it.Key == s.Backtrack.Key {
		return c.styles.bandLines("▶ ", it.Text, c.styles.selected.Render("  ↵ edit from here"), w)
	}

	return c.lines(it, w, s.Now, view{reasoning: s.ShowReasoning, details: s.Details})
}

// backtrackScroll is how far the transcript scrolls up so the selected
// message shows a third of the way down the window, or its top when it is
// taller; ok is false when no message is selected.
func backtrackScroll(s state.State, c *Cache, w, height int) (scroll int, ok bool) {
	if s.Backtrack == nil {
		return 0, false
	}
	below := 0
	for i, it := range slices.Backward(s.Items) {
		n := len(c.transcriptLines(s, i, w))
		if it.Key != s.Backtrack.Key {
			below += n

			continue
		}
		room := height - min(height/3, max(height-n, 0))

		return max(below+n-room, 0), true
	}

	return 0, false
}
