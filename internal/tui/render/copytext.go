package render

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"

	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// What a copy of selected text holds: what is drawn, cut by cells, minus
// the decoration around it (see docs/design/selection.md).

// SelectedText is the selected text as it is drawn at f's width, for the
// clipboard, and its number of lines. Each line keeps only its item's text
// (keep); blank lines at either end go.
func SelectedText(s state.State, c *Cache, f Frame) (string, int) {
	start, end, ok := s.SelectedRange()
	if !ok {
		return "", 0
	}
	var out []string
	add := func(key string, gutter int, lines []string) {
		keep := c.styles.keep(lines, gutter)
		for l, line := range lines {
			if from, to, ok := s.SelectedCols(key, l); ok {
				out = append(out, copyCells(ansi.Strip(line), max(from, keep[l][0]), min(to, keep[l][1])))
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

// keep are the cells [from, to) of each of an item's drawn lines that a
// copy keeps. Every line loses the item's gutter; each block of lines
// between blank ones loses the indent its lines share after it (a code
// block's or a table's padding, a tool row's indent); a code line loses
// the fence's language at its right end; and a quote loses its “ ” marks
// and the indent under the first.
func (st *Styles) keep(lines []string, gutter int) [][2]int {
	keep := make([][2]int, len(lines))
	quote := false
	for i := 0; i < len(lines); {
		j := i
		for j < len(lines) && strings.TrimSpace(ansi.Strip(lines[j])) != "" {
			j++
		}
		indent := sharedIndent(lines[i:j], gutter)
		for k := i; k < j; k++ {
			keep[k] = st.keepLine(lines[k], gutter+indent, &quote)
		}
		i = max(j, i+1)
	}

	return keep
}

// keepLine is the cells a copy keeps of one line from cell left on.
func (st *Styles) keepLine(line string, left int, quote *bool) [2]int {
	plain := ansi.Strip(line)
	text := strings.TrimRight(plain, " ")
	to := ansi.StringWidth(text)
	if bg := strings.Index(line, "\x1b[48;"); bg > 0 {
		if i := strings.LastIndex(line, st.dimOn); i > bg && isLabel(ansi.Strip(line[i:])) {
			to = min(to, ansi.StringWidth(ansi.Strip(line[:i]))) // the fence's language
		}
	}
	rest := ansi.Cut(plain, left, to)
	switch {
	case strings.HasPrefix(rest, "“ "):
		left, *quote = left+2, true
	case *quote && strings.HasPrefix(rest, "  "):
		left += 2
	}
	if *quote && strings.HasSuffix(text, " ”") {
		to, *quote = to-2, false
	}

	return [2]int{left, to}
}

// isLabel reports whether text is one word and spaces: a code block's
// language.
func isLabel(text string) bool {
	word := strings.TrimSpace(text)

	return word != "" && !strings.Contains(word, " ")
}

// sharedIndent is how many spaces all non-blank lines have after cell
// left.
func sharedIndent(lines []string, left int) int {
	indent := -1
	for _, l := range lines {
		plain := ansi.Strip(l)
		rest := ansi.Cut(plain, left, ansi.StringWidth(plain))
		n := len(rest) - len(strings.TrimLeft(rest, " "))
		if indent < 0 || n < indent {
			indent = n
		}
	}

	return max(indent, 0)
}

// copyCells is cells [from, to) of plain text, a wide character half
// inside counted whole, without trailing spaces.
func copyCells(plain string, from, to int) string {
	if from >= to {
		return ""
	}
	from, to = snap(plain, from, to)

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
