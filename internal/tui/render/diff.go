package render

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/viktordanov/uagent-harness/internal/patch"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// Diffs are drawn as Codex draws an applied patch, "Edited path (+3 -1)"
// and the changed lines with their numbers in a gutter, with Claude Code's
// tint across each whole added or removed line and its marks on the words
// that changed.

// compactDiffLines is how many diff lines the compact view shows per call
// before folding the rest behind ctrl+t.
const compactDiffLines = 12

// diffIndent is where a diff's lines start, under the tool line.
const diffIndent = "    "

// diffStyle is how one kind of diff line is drawn: its tint, the tint of
// its changed words, its sign, and its gutter.
type diffStyle struct {
	line, word, sign, gutter lipgloss.Style
}

func newDiffStyles(t Theme) map[string]diffStyle {
	tinted := func(bg, word, sign lipgloss.Style) diffStyle {
		return diffStyle{line: bg, word: word, sign: sign.Inherit(bg), gutter: lipgloss.NewStyle().Foreground(t.Dim).Inherit(bg)}
	}
	add, del := lipgloss.NewStyle().Background(t.DiffAdd), lipgloss.NewStyle().Background(t.DiffDel)

	return map[string]diffStyle{
		"+": tinted(add, lipgloss.NewStyle().Background(t.DiffAddWord), lipgloss.NewStyle().Foreground(t.Good)),
		"-": tinted(del, lipgloss.NewStyle().Background(t.DiffDelWord), lipgloss.NewStyle().Foreground(t.Bad)),
		" ": {line: lipgloss.NewStyle().Foreground(t.Dim), word: lipgloss.NewStyle().Foreground(t.Dim), sign: lipgloss.NewStyle(), gutter: lipgloss.NewStyle().Foreground(t.Dim)},
	}
}

// patchLines draws an apply_patch call with its diff: the tool line with
// Codex's summary, then the diff, folded after limit lines (0: unfolded).
func patchLines(it state.Item, w int, now time.Time, limit int) []string {
	head := it
	head.Label = ""
	out := []string{ansi.Truncate(compactTool(head, w, now)+diffSummary(it.Diff), w, "…")}

	return append(out, diffBlock(it.Diff, w, limit)...)
}

// diffSummary is Codex's header: "Edited a.go (+3 -1)", "Added b.go
// (+2 -0)", or "Edited 2 files (+5 -1)".
func diffSummary(files []patch.FileDiff) string {
	added, removed := 0, 0
	for _, f := range files {
		added, removed = added+f.Added, removed+f.Removed
	}
	if len(files) != 1 {
		return dim.Render(fmt.Sprintf("Edited %d files ", len(files))) + counts(added, removed)
	}
	verb := map[string]string{"add": "Added", "delete": "Deleted"}[files[0].Op]
	if verb == "" {
		verb = "Edited"
	}

	return dim.Render(verb+" ") + diffPath(files[0]) + " " + counts(added, removed)
}

func diffPath(f patch.FileDiff) string {
	if f.MovePath != "" {
		return f.Path + " → " + f.MovePath
	}

	return f.Path
}

// counts is "(+3 -1)" with the numbers in the diff's colors.
func counts(added, removed int) string {
	return dim.Render("(") + ok.Render(fmt.Sprintf("+%d", added)) + " " + bad.Render(fmt.Sprintf("-%d", removed)) + dim.Render(")")
}

// diffBlock draws the files' changed lines, each file under its own header
// when there are several. With limit > 0 it stops after that many lines
// and says how many more ctrl+t shows.
func diffBlock(files []patch.FileDiff, w, limit int) []string {
	gw := gutterWidth(files)
	var out []string
	shown, total := 0, 0
	for _, f := range files {
		total += f.Omitted
		if len(files) > 1 && (limit == 0 || shown < limit) {
			out = append(out, dim.Render("  └ ")+diffPath(f)+" "+counts(f.Added, f.Removed))
		}
		for i, h := range f.Hunks {
			total += len(h.Lines)
			if limit > 0 && shown >= limit {
				continue
			}
			if i > 0 {
				out = append(out, diffIndent+dim.Render(strings.Repeat(" ", gw)+" ⋮"))
			}
			lines := hunkLines(h, gw, w)
			if limit > 0 {
				lines = lines[:min(len(lines), limit-shown)]
			}
			shown += len(lines)
			out = append(out, lines...)
		}
	}
	switch {
	case total > shown && limit > 0:
		out = append(out, diffIndent+dim.Render(fmt.Sprintf("… +%d lines (ctrl+t to view)", total-shown)))
	case total > shown:
		out = append(out, diffIndent+dim.Render(fmt.Sprintf("… %d more lines not kept", total-shown)))
	}

	return out
}

// gutterWidth fits the largest line number the files show.
func gutterWidth(files []patch.FileDiff) int {
	n := 1
	for _, f := range files {
		for _, h := range f.Hunks {
			for _, l := range h.Lines {
				n = max(n, len(strconv.Itoa(l.Line())))
			}
		}
	}

	return n
}

// hunkLines draws a hunk, marking the changed words of each removed line
// that an added line replaced.
func hunkLines(h patch.DiffHunk, gw, w int) []string {
	segs := make([][]seg, len(h.Lines))
	for i := 0; i < len(h.Lines); {
		dels := run(h.Lines, i, "-")
		adds := run(h.Lines, i+dels, "+")
		for k := range min(dels, adds) {
			segs[i+k], segs[i+dels+k] = wordDiff(untab(h.Lines[i+k].Text), untab(h.Lines[i+dels+k].Text))
		}
		i += max(dels+adds, 1)
	}
	out := make([]string, 0, len(h.Lines))
	for i, l := range h.Lines {
		out = append(out, diffLine(l, segs[i], gw, w))
	}

	return out
}

// run counts the lines of kind from i.
func run(lines []patch.DiffLine, i int, kind string) int {
	n := 0
	for i+n < len(lines) && lines[i+n].Kind == kind {
		n++
	}

	return n
}

// diffLine draws "  12 +text", tinted to the full width for an added or
// removed line; segs, when set, mark its changed words.
func diffLine(l patch.DiffLine, segs []seg, gw, w int) string {
	st, known := diffStyles[l.Kind]
	if !known {
		st = diffStyles[" "]
	}
	if segs == nil {
		segs = []seg{{text: untab(l.Text)}}
	}
	head := diffIndent + st.gutter.Render(fmt.Sprintf("%*d ", gw, l.Line())) + st.sign.Render(l.Kind)
	room := max(w-ansi.StringWidth(head), 1)
	var b strings.Builder
	used := 0
	for _, s := range segs {
		text := s.text
		if width := ansi.StringWidth(text); used+width > room {
			text = ansi.Truncate(text, room-used, "…")
		}
		style := st.line
		if s.changed {
			style = st.word
		}
		b.WriteString(style.Render(text))
		if used += ansi.StringWidth(text); used >= room {
			break
		}
	}
	if l.Kind != " " {
		b.WriteString(st.line.Render(strings.Repeat(" ", max(room-used, 0))))
	}

	return head + b.String()
}
