package render

import (
	"fmt"
	"strings"

	"github.com/viktordanov/uah/internal/tui/state"
)

// questionTextRows is how many rows a question's text may take.
const questionTextRows = 4

// otherHelp is "None of the above"'s description.
const otherHelp = "Type your answer below."

// questionLines draw the agent's questions above the composer, after
// Codex's request_user_input overlay and Claude Code's AskUserQuestion, in
// the panel an approval shares: the shown question's header in the frame
// (and which of how many), the headers as tabs when there are several, the
// question, its options numbered with their descriptions and the chosen
// one in the accent band, then "None of the above", and the keys.
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
	rows := state.Rows(question)
	keys, labels, helps := make([]string, rows), make([]string, rows), make([]string, rows)
	for i := range rows {
		keys[i], labels[i], helps[i] = fmt.Sprintf("%d.", i+1), state.ChoiceLabel(question, i), otherHelp
		if i < len(question.Options) {
			helps[i] = oneLine(question.Options[i].Description)
		}
	}
	body = append(body, "")
	body = append(body, st.choiceRows(keys, labels, helps, q.Choice[q.Current], inner)...)

	return st.panel(title, st.accent, body, questionHint(q), w)
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
	hint := fmt.Sprintf("↑↓ choose · 1-%d pick · type your own · %s", min(state.Rows(q.Questions[q.Current]), 9), enter)
	if len(q.Questions) > 1 {
		hint += " · tab next"
	}

	return hint + " · esc interrupt"
}
