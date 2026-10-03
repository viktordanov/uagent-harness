package render

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/viktordanov/uah/internal/engine"
	"github.com/viktordanov/uah/internal/tui/state"
)

const (
	// questionTextRows is how many rows a question's text may take.
	questionTextRows = 4
	// previewRows is how many rows of a preview show; the rest is "… N
	// more lines".
	previewRows = 14
	// previewSideBySide is the narrowest panel body that shows a preview
	// beside the options; a narrower one shows it under them.
	previewSideBySide = 96
	// ownLabel is the last row: an answer in the user's own words.
	ownLabel = "Type your own answer"
	// noteMark marks an option with a note.
	noteMark = "✎"
)

// questionLines draw the agent's questions above the composer, after
// Codex's request_user_input overlay and Claude Code's AskUserQuestion, in
// the panel an approval shares: the shown question's header in the frame
// (and which of how many), the headers as tabs when there are several, the
// question, its options numbered with their descriptions and notes, the
// chosen one in the accent band, then "Type your own answer", and the
// keys. Options with previews show the chosen one's preview beside the
// list, or under it on a narrow screen.
func (st *Styles) questionLines(q *state.Questions, w int) []string {
	question := q.Questions[q.Current]
	title := "? " + question.Header
	if n := len(q.Questions); n > 1 {
		title += fmt.Sprintf(" · %d of %d", q.Current+1, n)
	}
	inner := max(w-4, panelMin)
	var body []string
	if len(q.Questions) > 1 {
		body = append(body, st.questionTabs(q))
	}
	text := wrapPrefixed(oneLine(question.Question), inner, "", "")
	if len(text) > questionTextRows {
		text = append(text[:questionTextRows-1], text[questionTextRows-1]+"…")
	}
	body = append(body, styleLines(text, st.bold)...)
	if q.Sent {
		return st.panel(title, st.accent, body, "sending the answers…", w)
	}
	body = append(body, "")
	switch {
	case !hasPreview(question):
		body = append(body, st.questionRows(q, inner, true)...)
	case inner < previewSideBySide:
		body = append(body, st.questionRows(q, inner, true)...)
		body = append(body, "")
		body = append(body, st.previewBox(q, inner)...)
	default:
		body = append(body, st.sideBySide(q, inner)...)
	}

	return st.panel(title, st.accent, body, questionHint(q), w)
}

// sideBySide puts the options, each with its help under it, on the left
// two fifths, and the chosen one's preview on the right.
func (st *Styles) sideBySide(q *state.Questions, w int) []string {
	left := min(max(w*2/5, 30), w-30)
	rows := st.questionRows(q, left, false)
	box := st.previewBox(q, w-left-2)
	out := make([]string, 0, max(len(rows), len(box)))
	for i := range max(len(rows), len(box)) {
		line := ""
		if i < len(rows) {
			line = ansi.Truncate(rows[i], left, "…")
		}
		line += strings.Repeat(" ", left-ansi.StringWidth(line)) + "  "
		if i < len(box) {
			line += box[i]
		}
		out = append(out, line)
	}

	return out
}

// questionRows are the shown question's rows: each option, its help after
// it (inline) or under it, and its note under it, then "Type your own
// answer" with what the user typed there.
func (st *Styles) questionRows(q *state.Questions, w int, inline bool) []string {
	question := q.Questions[q.Current]
	n := state.Rows(question)
	keys, labels, helps := make([]string, n), make([]string, n), make([]string, n)
	for i := range n {
		keys[i] = fmt.Sprintf("%d.", i+1)
		if i == len(question.Options) {
			labels[i], helps[i] = ownLabel, oneLine(q.Own[q.Current])

			continue
		}
		labels[i], helps[i] = question.Options[i].Label, oneLine(question.Options[i].Description)
		if q.Notes[q.Current][i] != "" || q.Noting == i {
			labels[i] += " " + noteMark
		}
	}
	under := make([]string, n) // help under the row instead of after it
	if !inline {
		copy(under, helps[:len(question.Options)])
		helps = make([]string, n)
		helps[n-1] = oneLine(q.Own[q.Current])
	}
	var out []string
	for i, row := range st.choiceRows(keys, labels, helps, q.Choice[q.Current], w) {
		out = append(out, row)
		if under[i] != "" {
			out = append(out, st.dim.Render("     "+ansi.Truncate(under[i], max(w-5, 1), "…")))
		}
		out = append(out, st.noteLines(q, i, w)...)
	}

	return out
}

// noteLines are an option's note under it, dim, or a hint while it is
// being written in the composer.
func (st *Styles) noteLines(q *state.Questions, row, w int) []string {
	if row >= len(q.Notes[q.Current]) {
		return nil
	}
	note := q.Notes[q.Current][row]
	switch {
	case q.Noting == row:
		note = "writing a note below…"
	case note == "":
		return nil
	}

	return []string{st.dim.Render(ansi.Truncate("     "+noteMark+" "+oneLine(note), w, "…"))}
}

// hasPreview reports whether any option of the question has a preview.
func hasPreview(q engine.Question) bool {
	for _, o := range q.Options {
		if o.Preview != "" {
			return true
		}
	}

	return false
}

// previewBox frames the chosen option's preview, w wide: its lines in the
// terminal's own color, cut to the width, and at most previewRows of them
// with "… N more lines" after.
func (st *Styles) previewBox(q *state.Questions, w int) []string {
	question := q.Questions[q.Current]
	row := q.Choice[q.Current]
	title, text := "Preview", ""
	if row < len(question.Options) {
		title, text = question.Options[row].Label, question.Options[row].Preview
	}
	var lines []string
	switch {
	case row == len(question.Options):
		lines = []string{st.dim.Render("Your own answer, typed below.")}
	case text == "":
		lines = []string{st.dim.Render("No preview for this option.")}
	default:
		all := strings.Split(strings.TrimRight(untab(text), "\n"), "\n")
		for i, l := range all {
			if i == previewRows {
				lines = append(lines, st.dim.Render(fmt.Sprintf("… %d more lines", len(all)-previewRows)))

				break
			}
			lines = append(lines, l)
		}
	}
	room := max(w-4, 1)
	label := ansi.Truncate(title, max(w-6, 1), "…")
	out := []string{st.dim.Render("┌ ") + st.bold.Render(label) + st.dim.Render(" "+strings.Repeat("─", max(w-4-ansi.StringWidth(label), 0))+"┐")}
	for _, l := range lines {
		l = ansi.Truncate(l, room, "…")
		out = append(out, st.dim.Render("│ ")+l+strings.Repeat(" ", room-ansi.StringWidth(l))+st.dim.Render(" │"))
	}

	return append(out, st.dim.Render("└"+strings.Repeat("─", max(w-2, 0))+"┘"))
}

// questionTabs are the headers: the shown one in the accent band, an
// answered one with ✓, the others dim.
func (st *Styles) questionTabs(q *state.Questions) string {
	tabs := make([]string, 0, len(q.Questions))
	for i, question := range q.Questions {
		switch {
		case i == q.Current:
			tabs = append(tabs, st.selected.Render(" "+question.Header+" "))
		case q.Done[i]:
			tabs = append(tabs, st.dim.Render("✓ "+question.Header))
		default:
			tabs = append(tabs, st.dim.Render("  "+question.Header))
		}
	}

	return strings.Join(tabs, " ")
}

// questionHint names the keys of what has focus: a note being written,
// the own-answer row, or the options. Enter answers the question, or sends
// the answers on the last one unanswered.
func questionHint(q *state.Questions) string {
	if q.Noting >= 0 {
		return "enter keep the note · esc drop it"
	}
	enter := "enter send"
	for i, done := range q.Done {
		if !done && i != q.Current {
			enter = "enter answer"

			break
		}
	}
	next := ""
	if len(q.Questions) > 1 {
		next = " · tab next"
	}
	if q.OwnRow() {
		return "type your answer · " + enter + " · ↑ back" + next + " · esc interrupt"
	}

	return fmt.Sprintf("↑↓ choose · 1-%d pick · n note · %s%s · esc interrupt", min(state.Rows(q.Questions[q.Current]), 9), enter, next)
}
