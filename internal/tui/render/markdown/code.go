package markdown

import (
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// codeBlock draws code on the band, highlighted and not wrapped, as Codex
// does, with the fence's language dim at the right end of its first line.
// A diff's + and - lines sit on the edit tool's tints instead of the band.
// While a fence is still open its last line is usually incomplete: the
// complete lines are highlighted as one piece, which stays cached until the
// next newline, and the partial line on its own.
func (r *Renderer) codeBlock(lang, code string, w int) []string {
	var hl []string
	if i := strings.LastIndexByte(code, '\n'); i >= 0 && i < len(code)-1 {
		hl = append(slices.Clip(r.highlight(lang, code[:i])), r.highlight(lang, code[i+1:])...)
	} else {
		hl = r.highlight(lang, strings.TrimSuffix(code, "\n"))
	}
	var src []string
	if isDiff(lang) {
		src = strings.Split(strings.TrimSuffix(code, "\n"), "\n")
	}
	out := make([]string, len(hl))
	for i, l := range hl {
		line := " " + l
		if i == 0 {
			line = r.labeled(line, lang, w)
		}
		band := r.st.Band
		if i < len(src) {
			band = r.tint(src[i])
		}
		out[i] = band(line, w)
	}

	return out
}

// labeled is a code block's first line with the language dim at the right
// end of width w, a cell from the edge. A line that leaves no space before
// the label goes without it.
func (r *Renderer) labeled(line, lang string, w int) string {
	gap := w - ansi.StringWidth(line) - ansi.StringWidth(lang) - 1
	if lang == "" || gap < 1 {
		return line
	}

	return line + strings.Repeat(" ", gap) + r.st.Dim.Render(lang) + " "
}

// tint is how a diff line is drawn: on the added or removed tint by its
// first character, else on the band.
func (r *Renderer) tint(line string) func(string, int) string {
	switch {
	case strings.HasPrefix(line, "+"):
		return r.st.Added
	case strings.HasPrefix(line, "-"):
		return r.st.Removed
	}

	return r.st.Band
}

// isDiff reports whether a fence's language is one of chroma's names for
// a diff.
func isDiff(lang string) bool {
	switch strings.ToLower(lang) {
	case "diff", "patch", "udiff":
		return true
	}

	return false
}
