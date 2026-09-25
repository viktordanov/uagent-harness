package render

import (
	"slices"
	"strings"

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

// fade dims the window's lines outside the selected message while going
// back, so the message stands out. The selected message is lines [from,
// from+n) of the transcript, and the window starts at its line start. The
// cache keeps the items' own lines; fading draws over copies each frame, so
// entering or leaving the selection renders nothing again.
func (st *Styles) fade(window []string, start, from, n int) {
	for j, l := range window {
		if k := start + j; l != "" && (k < from || k >= from+n) {
			window[j] = st.faded(l)
		}
	}
}

// faded is a line in the dim color: its colors and weights give way to the
// theme's dim, and only backgrounds, such as the band, stay.
func (st *Styles) faded(line string) string {
	return st.dimOn + sgr.ReplaceAllStringFunc(line, st.fadeSGR) + "\x1b[m"
}

// fadeSGR keeps an SGR's resets and backgrounds and drops the rest; after
// a reset the dim foreground comes back.
func (st *Styles) fadeSGR(seq string) string {
	params := strings.Split(seq[2:len(seq)-1], ";")
	var kept []string
	reset := false
	for i := 0; i < len(params); i++ {
		switch p := params[i]; p {
		case "", "0":
			kept, reset = append(kept, "0"), true
		case "38", "48", "58":
			n := extendedColorLen(params[i+1:])
			if p == "48" {
				kept = append(kept, params[i:i+1+n]...)
			}
			i += n
		default:
			if isBackground(p) {
				kept = append(kept, p)
			}
		}
	}
	if reset {
		kept = append(kept, st.dimOn[2:len(st.dimOn)-1])
	}
	if len(kept) == 0 {
		return ""
	}

	return "\x1b[" + strings.Join(kept, ";") + "m"
}

// extendedColorLen is how many parameters follow 38, 48, or 58: 5;n or
// 2;r;g;b.
func extendedColorLen(rest []string) int {
	switch {
	case len(rest) > 0 && rest[0] == "5":
		return min(2, len(rest))
	case len(rest) > 0 && rest[0] == "2":
		return min(4, len(rest))
	}

	return 0
}

// isBackground reports whether an SGR parameter sets or clears the
// background: 40–47, 49, or 100–107.
func isBackground(p string) bool {
	return (len(p) == 2 && p[0] == '4' && p[1] != '8') || (len(p) == 3 && p[:2] == "10" && p[2] <= '7')
}
