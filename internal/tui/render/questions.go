package render

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/viktordanov/uah/internal/tui/state"
)

// questionTextRows is how many rows a question's text may take.
const questionTextRows = 4

// otherHelp is "None of the above"'s description.
const otherHelp = "Type your answer below."

// questionLines draw the agent's questions above the composer, after
// Codex's request_user_input overlay and Claude Code's AskUserQuestion: the
// questions' headers as tabs, the shown question, its options numbered with
// their descriptions dim and the chosen one in the accent band, then "None
// of the above", and the keys.
func (st *Styles) questionLines(q *state.Questions, w int) []string {
	question := q.Questions[q.Current]
	out := []string{ansi.Truncate(st.accent.Render(" ? ")+st.questionTabs(q), w, "…")}
	text := wrapPrefixed(oneLine(question.Question), w, " ", " ")
	if len(text) > questionTextRows {
		text = append(text[:questionTextRows-1], ansi.Truncate(text[questionTextRows-1], w-1, "")+"…")
	}
	out = append(out, styleLines(text, st.bold)...)
	if q.Sent {
		return append(out, st.dim.Render(" sending the answers…"))
	}
	rows := state.Rows(question)
	labels, helps := make([]string, rows), make([]string, rows)
	width := 0
	for i := range rows {
		labels[i] = fmt.Sprintf("%d. %s", i+1, state.ChoiceLabel(question, i))
		helps[i] = otherHelp
		if i < len(question.Options) {
			helps[i] = oneLine(question.Options[i].Description)
		}
		width = max(width, ansi.StringWidth(labels[i]))
	}
	width = min(width, max(w/2, 12)) // a long label leaves its description some room
	for i := range rows {
		label := ansi.Truncate(labels[i], width, "…")
		label += strings.Repeat(" ", width-ansi.StringWidth(label))
		line := "   " + label + "  " + st.dim.Render(helps[i])
		if i == q.Choice[q.Current] {
			line = st.selected.Render(" › "+label) + "  " + st.dim.Render(helps[i])
		}
		out = append(out, ansi.Truncate(line, w, "…"))
	}

	return append(out, st.dim.Render(ansi.Truncate(questionHint(q), w, "…")))
}

// questionTabs are the headers: the shown one in the accent band, an
// answered one with ✓, the others dim.
func (st *Styles) questionTabs(q *state.Questions) string {
	if len(q.Questions) == 1 {
		return st.bold.Render(q.Questions[0].Header)
	}
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

// questionHint names the keys: enter answers the question, or sends the
// answers on the last one unanswered.
func questionHint(q *state.Questions) string {
	enter := "enter send"
	for i, done := range q.Done {
		if !done && i != q.Current {
			enter = "enter answer"

			break
		}
	}
	hint := fmt.Sprintf("   ↑↓ choose · 1-%d pick · type your own · %s", state.Rows(q.Questions[q.Current]), enter)
	if len(q.Questions) > 1 {
		hint += " · tab next question"
	}

	return hint + " · esc interrupt"
}
