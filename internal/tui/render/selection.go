package render

import (
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"

	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// Text selected with the mouse (state.Selection): each frame records which
// transcript line every row of the window shows, so the shell can turn the
// mouse into a state.TextPos (At), and draws the selection over the
// window's lines as fade does, so the cache keeps each item's own lines.

// rowRefs are the transcript lines of all, a transcript's lines built from
// rev (each item's lines, bottom item first, with its key in keys) after
// head lines of banner.
func rowRefs(keys []string, rev [][]string, head int) []state.TextPos {
	refs := make([]state.TextPos, 0, head+len(rev)*2)
	for l := range head {
		refs = append(refs, state.TextPos{Key: state.BannerKey, Line: l})
	}
	for i, lines := range slices.Backward(rev) {
		for l := range lines {
			refs = append(refs, state.TextPos{Key: keys[i], Line: l})
		}
	}

	return refs
}

// selectWindow records the window's rows, refs for its last lines (the
// first pad rows are empty), and draws the selection over them.
func (c *Cache) selectWindow(s state.State, out []string, refs []state.TextPos) {
	pad := len(out) - len(refs)
	c.rows, c.window = make([]state.TextPos, len(out)), out
	copy(c.rows[pad:], refs)
	if s.Selection == nil {
		return
	}
	for j := pad; j < len(out); j++ {
		if from, to, ok := s.SelectedCols(c.rows[j].Key, c.rows[j].Line); ok {
			out[j] = c.styles.highlight(out[j], from, to)
		}
	}
}

// highlight draws cells [from, to) of a line on the selection's
// background, in the terminal's own text color; the rest keeps its styles.
func (st *Styles) highlight(line string, from, to int) string {
	from, to = snap(ansi.Strip(line), from, to)
	if from >= to {
		return line
	}
	before := ansi.Truncate(line, to, "")
	// The styles in force where the selection ends carry on after it.
	resume := strings.Join(sgr.FindAllString(before, -1), "")

	return ansi.Truncate(line, from, "") + "\x1b[m" + st.selectOn + ansi.Strip(ansi.Cut(line, from, to)) + "\x1b[m" +
		resume + ansi.TruncateLeft(line, to, "")
}

// At is the transcript cell drawn at screen cell (x, y) in the last frame,
// and its line's text without styles. ok is false outside the transcript
// and on the empty rows above a short one. Clamp moves a y above or below
// the transcript to its first or last line, for a drag past its edge.
func (c *Cache) At(x, y int, clamp bool) (pos state.TextPos, text string, ok bool) {
	row := y - c.top
	if clamp {
		first := 0
		for first < len(c.rows) && c.rows[first].Key == "" {
			first++
		}
		row = min(max(row, first), len(c.rows)-1)
	}
	if row < 0 || row >= len(c.rows) || c.rows[row].Key == "" {
		return state.TextPos{}, "", false
	}
	pos = c.rows[row]
	pos.Col = max(x, 0)

	return pos, ansi.Strip(c.window[row]), true
}

// Edge says where screen row y lies from the last frame's transcript: -1
// on its first row or above, 1 below it, and 0 inside, so a drag to the top
// row scrolls up even where the transcript starts at the screen's top.
func (c *Cache) Edge(y int) int {
	switch {
	case y <= c.top:
		return -1
	case y >= c.top+len(c.rows):
		return 1
	}

	return 0
}

// SelectedText is the selected text as it is drawn at f's width, for the
// clipboard, and its number of lines. Each line loses its trailing spaces
// and its item's decoration: the mark and indent before your message, a
// command, the agent's answer, reasoning, and warnings, and the band's
// padding before a code line; blank lines at either end go.
func SelectedText(s state.State, c *Cache, f Frame) (string, int) {
	start, end, ok := s.SelectedRange()
	if !ok {
		return "", 0
	}
	var out []string
	add := func(key string, gutter int, lines []string) {
		for l, line := range lines {
			if from, to, ok := s.SelectedCols(key, l); ok {
				out = append(out, c.styles.copyLine(line, gutter, from, to))
			}
		}
	}
	if start.Key == state.BannerKey {
		add(state.BannerKey, 0, c.styles.banner(s, f.Version, f.Width))
	}
	for i, it := range s.Items {
		if it.Key == start.Key || len(out) > 0 {
			add(it.Key, gutter(it), c.transcriptLines(s, i, f.Width))
		}
		if it.Key == end.Key {
			break
		}
	}
	for len(out) > 0 && out[0] == "" {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}

	return strings.Join(out, "\n"), len(out)
}

// copyLine is cells [from, to) of a drawn line as text: without its first
// gutter cells, and on a code line (a background after the indent) the
// band's padding and the language at its right end, and without trailing
// spaces.
func (st *Styles) copyLine(line string, gutter, from, to int) string {
	if bg := strings.Index(line, "\x1b[48;"); gutter > 0 && bg > 0 {
		gutter++ // a code line: its band starts after the indent, with a space
		if i := strings.LastIndex(line, st.dimOn); i > bg && strings.TrimSpace(ansi.Strip(line[i:])) != "" &&
			!strings.Contains(strings.TrimSpace(ansi.Strip(line[i:])), " ") {
			to = min(to, ansi.StringWidth(ansi.Strip(line[:i]))) // the fence's language, dim
		}
	}
	plain := ansi.Strip(line)
	from, to = snap(plain, max(from, gutter), to)

	return strings.TrimRight(ansi.Cut(plain, from, to), " ")
}

// snap widens cells [from, to) of text to whole characters, so a wide
// character half inside counts, and ends it at the text's end.
func snap(text string, from, to int) (int, int) {
	start, end, at := from, 0, 0
	g := uniseg.NewGraphemes(text)
	for g.Next() {
		next := at + g.Width()
		if at <= from && from < next {
			start = at
		}
		if at < to {
			end = next
		}
		at = next
	}

	return start, min(end, at)
}

// gutter is how many cells before an item's text are decoration: the λ or
// ! mark and the indent under it, the agent's •, reasoning's ~, and a
// warning's or an error's mark.
func gutter(it state.Item) int {
	switch it.Kind {
	case state.KindUser, state.KindShell, state.KindAssistant:
		return 2
	case state.KindReasoning:
		return 4
	case state.KindNotice:
		if it.Level == session.LevelWarning || it.Level == session.LevelError {
			return 4
		}

		return 2
	}

	return 0
}
