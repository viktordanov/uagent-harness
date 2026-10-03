package state

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/viktordanov/uah/internal/engine"
	"github.com/viktordanov/uah/internal/session"
)

// The agent's questions (request_user_input) show as a picker above the
// composer, after Codex's and Claude Code's: one question at a time, its
// options numbered, then "Type your own answer". On an option, ↑ and ↓
// choose, a number picks and answers, n writes a note on the option in
// the composer (enter keeps it, esc drops it), and other letters do
// nothing, so no keystroke starts an answer by accident. On "Type your own
// answer" the composer holds the answer. Enter answers the question and
// goes to the next unanswered one, or sends every answer; tab and
// shift+tab go between the questions; esc interrupts the run, as in Codex.
// See docs/design/questions.md.

// Questions are the agent's questions waiting for the user's answers.
type Questions struct {
	ID        string
	CallID    string
	Questions []engine.Question
	// Current is the question shown.
	Current int
	// Choice is each question's chosen row: an option's index, or
	// len(Options) for "Type your own answer".
	Choice []int
	// Notes are each question's notes, by option.
	Notes [][]string
	// Own are each question's answers in the user's own words, kept while
	// another row or question shows; on its row, the composer holds it.
	Own []string
	// Noting is the option whose note the composer holds (-1: none).
	Noting int
	// Done marks the answered questions.
	Done []bool
	// Sent hides the choices once the answers are on their way.
	Sent  bool
	Since time.Time
}

// Intents in the questions picker. Draft is the composer's text.
type (
	// QuestionMove moves the shown question's choice.
	QuestionMove struct {
		Delta int
		Draft string
	}
	// QuestionPick chooses a row by its number (1 is the first): an
	// option answers the question, the last row takes the user's words.
	QuestionPick struct{ Number int }
	// QuestionNote starts a note on the chosen option.
	QuestionNote struct{}
	// QuestionSwitch shows the next (+1) or previous (-1) question.
	QuestionSwitch struct {
		Delta int
		Draft string
	}
	// QuestionAnswer is enter: it keeps a note being written, or answers
	// the shown question with its choice.
	QuestionAnswer struct{ Draft string }
	// QuestionDismiss is esc: it drops a note being written, or
	// interrupts the run, which cancels the questions.
	QuestionDismiss struct{}
)

// EffAnswerQuestions sends the answers.
type EffAnswerQuestions struct {
	ID      string
	Answers engine.Answers
}

func (EffAnswerQuestions) effect() {}

// PendingQuestions are the questions the picker shows, if any.
func (s State) PendingQuestions() (*Questions, bool) {
	if len(s.Questions) == 0 {
		return nil, false
	}

	return &s.Questions[0], true
}

// Rows is the number of rows a question offers: its options and "Type
// your own answer".
func Rows(q engine.Question) int { return len(q.Options) + 1 }

// OwnRow reports whether the shown question's choice is "Type your own
// answer", where the composer holds the answer.
func (q *Questions) OwnRow() bool { return q.Choice[q.Current] == len(q.Questions[q.Current].Options) }

// Typing reports whether keys go to the composer: a note being written,
// or an answer in the user's own words.
func (q *Questions) Typing() bool { return !q.Sent && (q.Noting >= 0 || q.OwnRow()) }

func (s *State) askQuestions(e session.QuestionsAsked) {
	n := len(e.Questions)
	q := Questions{
		ID: e.ID, CallID: e.CallID, Questions: e.Questions, Choice: make([]int, n), Notes: make([][]string, n),
		Own: make([]string, n), Noting: -1, Done: make([]bool, n), Since: e.At,
	}
	for i, question := range e.Questions {
		q.Notes[i] = make([]string, len(question.Options))
	}
	s.Questions = append(s.Questions, q)
	s.Scroll = 0
}

// closeQuestions drops answered or canceled questions.
func (s *State) closeQuestions(e session.QuestionsAnswered) {
	for i, q := range s.Questions {
		if q.ID == e.ID {
			s.Questions = append(s.Questions[:i:i], s.Questions[i+1:]...)

			return
		}
	}
}

// onQuestions handles the picker's intents.
func (s *State) onQuestions(ev any) ([]Effect, bool) {
	q, ok := s.PendingQuestions()
	if !ok {
		return nil, false
	}
	if q.Sent {
		switch ev.(type) {
		case QuestionMove, QuestionPick, QuestionNote, QuestionSwitch, QuestionAnswer:
			return nil, true
		}
	}
	switch e := ev.(type) {
	case QuestionMove:
		if q.Noting >= 0 {
			return nil, true
		}
		rows := Rows(q.Questions[q.Current])

		return q.choose((q.Choice[q.Current]+e.Delta+rows)%rows, e.Draft), true
	case QuestionPick:
		return q.pick(e.Number), true
	case QuestionNote:
		if q.Noting >= 0 || q.OwnRow() {
			return nil, true
		}
		q.Noting = q.Choice[q.Current]

		return []Effect{EffSetDraft{Text: q.Notes[q.Current][q.Noting]}}, true
	case QuestionSwitch:
		q.leave(e.Draft)
		n := len(q.Questions)
		q.Current = (q.Current + e.Delta + n) % n

		return []Effect{EffSetDraft{Text: q.draft()}}, true
	case QuestionAnswer:
		return q.enter(e.Draft), true
	case QuestionDismiss:
		if q.Noting >= 0 {
			q.Noting = -1

			return []Effect{EffSetDraft{}}, true
		}

		return s.interrupt(), true
	default:
		return nil, false
	}
}

// pick chooses a row by its number: an option answers at once, the own
// row takes the composer.
func (q *Questions) pick(number int) []Effect {
	if q.Noting >= 0 || number < 1 || number > Rows(q.Questions[q.Current]) {
		return nil
	}
	effects := q.choose(number-1, "")
	if q.OwnRow() {
		return effects
	}

	return append(effects, q.answer("")...)
}

// enter keeps a note being written, or answers the shown question; on the
// own row only once something is typed.
func (q *Questions) enter(draft string) []Effect {
	switch {
	case q.Noting >= 0:
		q.Notes[q.Current][q.Noting] = strings.TrimSpace(draft)
		q.Noting = -1

		return []Effect{EffSetDraft{}}
	case q.OwnRow() && strings.TrimSpace(draft) == "":
		return nil
	}

	return q.answer(draft)
}

// choose moves the shown question's choice to row: the composer takes the
// row's own answer, or empties when leaving it.
func (q *Questions) choose(row int, draft string) []Effect {
	q.leave(draft)
	q.Choice[q.Current] = row

	return []Effect{EffSetDraft{Text: q.draft()}}
}

// leave keeps the composer's text when the own-answer row loses it, and
// drops a note being written.
func (q *Questions) leave(draft string) {
	if q.Noting >= 0 {
		q.Noting = -1

		return
	}
	if q.OwnRow() {
		q.Own[q.Current] = draft
	}
}

// draft is what the composer holds for the shown question: its own
// answer on that row, else nothing.
func (q *Questions) draft() string {
	if q.OwnRow() {
		return q.Own[q.Current]
	}

	return ""
}

// answer answers the shown question, with draft as the answer on the own
// row, then shows the next unanswered one, or sends the answers once every
// question has one.
func (q *Questions) answer(draft string) []Effect {
	if q.OwnRow() {
		q.Own[q.Current] = strings.TrimSpace(draft)
	}
	q.Done[q.Current] = true
	for i := range len(q.Questions) {
		next := (q.Current + 1 + i) % len(q.Questions)
		if !q.Done[next] {
			q.Current = next

			return []Effect{EffSetDraft{Text: q.draft()}}
		}
	}
	q.Sent = true

	return []Effect{EffSetDraft{}, EffAnswerQuestions{ID: q.ID, Answers: q.Answers()}}
}

// Answers are the answers in Codex's encoding: the chosen option's label
// and its note as "user_note: …", or "None of the above" and the user's
// own words as the note.
func (q *Questions) Answers() engine.Answers {
	out := engine.Answers{}
	for i, question := range q.Questions {
		if !q.Done[i] {
			out[question.ID] = engine.Answer{Answers: []string{}}

			continue
		}
		row, note := q.Choice[i], q.Own[i]
		if row < len(question.Options) {
			note = q.Notes[i][row]
		}
		answers := []string{ChoiceLabel(question, row)}
		if note != "" {
			answers = append(answers, engine.NotePrefix+note)
		}
		out[question.ID] = engine.Answer{Answers: answers}
	}

	return out
}

// ChoiceLabel is a row's answer: an option's label, or "None of the
// above", Codex's answer for one in the user's own words.
func ChoiceLabel(q engine.Question, row int) string {
	if row >= 0 && row < len(q.Options) {
		return q.Options[row].Label
	}

	return engine.OtherAnswer
}

// onQuestionsAnswered puts the answers under the call's line, one per
// question: its header and the answer, live and in a loaded transcript.
func (s *State) onQuestionsAnswered(e engine.QuestionsAnswered) {
	s.update("call:"+e.CallID, func(it *Item) { it.Answers = AnswerLines(e.Questions, e.Answers) })
}

// AnswerLines say what the user answered, a line per question: "Header:
// label · note", or the user's own words alone.
func AnswerLines(questions []engine.Question, answers engine.Answers) []string {
	out := make([]string, 0, len(questions))
	for _, q := range questions {
		var parts []string
		for _, a := range answers[q.ID].Answers {
			if note, ok := strings.CutPrefix(a, engine.NotePrefix); ok {
				a = oneLine(note)
			}
			parts = append(parts, a)
		}
		if len(parts) == 2 && parts[0] == engine.OtherAnswer {
			parts = parts[1:] // the user's own words
		}
		if len(parts) == 0 {
			parts = []string{"no answer"}
		}
		out = append(out, q.Header+": "+strings.Join(parts, " · "))
	}

	return out
}

// questionParts is a request_user_input call's line: its one question, or
// the headers of several.
func questionParts(arguments string) string {
	var args struct {
		Questions []engine.Question `json:"questions"`
	}
	if json.Unmarshal([]byte(arguments), &args) != nil || len(args.Questions) == 0 {
		return ""
	}
	if len(args.Questions) == 1 {
		return oneLine(args.Questions[0].Question)
	}
	headers := make([]string, 0, len(args.Questions))
	for _, q := range args.Questions {
		headers = append(headers, q.Header)
	}

	return strings.Join(headers, ", ")
}
