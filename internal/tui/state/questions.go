package state

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/viktordanov/uah/internal/engine"
	"github.com/viktordanov/uah/internal/session"
)

// The agent's questions (request_user_input) show as a picker above the
// composer, after Codex's and Claude Code's: one question at a time, its
// options numbered, then "None of the above". ↑ and ↓ choose, a number
// picks, and what the composer holds is the answer's note: typing on an
// option the user did not choose moves to "None of the above", so a typed
// answer stands on its own. Enter answers the question and goes to the
// next unanswered one, or sends every answer; tab and shift+tab go between
// the questions; esc interrupts the run, as in Codex, and the composer is
// free again. See docs/design/questions.md.

// Questions are the agent's questions waiting for the user's answers.
type Questions struct {
	ID        string
	CallID    string
	Questions []engine.Question
	// Current is the question shown.
	Current int
	// Choice is each question's chosen row: an option's index, or
	// len(Options) for "None of the above".
	Choice []int
	// Chosen marks the questions whose row the user chose, with a key or
	// a number; typing then keeps the row.
	Chosen []bool
	// Notes are each question's text, kept while another question shows;
	// the shown question's is the composer's.
	Notes []string
	// Done marks the answered questions.
	Done []bool
	// Sent hides the choices once the answers are on their way.
	Sent  bool
	Since time.Time
}

// Intents in the questions picker.
type (
	// QuestionMove moves the shown question's choice.
	QuestionMove struct{ Delta int }
	// QuestionPick chooses a row by its number (1 is the first) and
	// answers the question.
	QuestionPick struct{ Number int }
	// QuestionSwitch shows the next (+1) or previous (-1) question; Draft
	// is the composer's text, the shown question's note.
	QuestionSwitch struct {
		Delta int
		Draft string
	}
	// QuestionAnswer answers the shown question with its choice and Draft
	// as its note.
	QuestionAnswer struct{ Draft string }
	// QuestionDismiss interrupts the run, which cancels the questions.
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

// Rows is the number of rows a question offers: its options and "None of
// the above".
func Rows(q engine.Question) int { return len(q.Options) + 1 }

func (s *State) askQuestions(e session.QuestionsAsked) {
	n := len(e.Questions)
	s.Questions = append(s.Questions, Questions{
		ID: e.ID, CallID: e.CallID, Questions: e.Questions, Choice: make([]int, n), Chosen: make([]bool, n),
		Notes: make([]string, n), Done: make([]bool, n), Since: e.At,
	})
	s.Scroll = 0
}

// closeQuestions drops answered or canceled questions.
func (s *State) closeQuestions(e session.QuestionsAnswered) {
	s.Questions = slices.DeleteFunc(s.Questions, func(q Questions) bool { return q.ID == e.ID })
}

// onQuestions handles the picker's intents, and the composer's text while
// it shows.
func (s *State) onQuestions(ev any) ([]Effect, bool) {
	q, ok := s.PendingQuestions()
	if !ok {
		return nil, false
	}
	if d, isDraft := ev.(DraftChanged); isDraft {
		// Typing on an option the user did not choose answers in their own
		// words; the menu still sees the draft.
		if d.Draft != "" && !q.Chosen[q.Current] {
			q.Choice[q.Current] = len(q.Questions[q.Current].Options)
		}

		return nil, false
	}
	if q.Sent {
		switch ev.(type) {
		case QuestionMove, QuestionPick, QuestionSwitch, QuestionAnswer:
			return nil, true
		}
	}
	switch e := ev.(type) {
	case QuestionMove:
		rows := Rows(q.Questions[q.Current])
		q.Choice[q.Current] = (q.Choice[q.Current] + e.Delta + rows) % rows
		q.Chosen[q.Current] = true
	case QuestionPick:
		if e.Number < 1 || e.Number > Rows(q.Questions[q.Current]) {
			return nil, true
		}
		q.Choice[q.Current], q.Chosen[q.Current] = e.Number-1, true

		return q.answer(q.Notes[q.Current]), true
	case QuestionSwitch:
		n := len(q.Questions)
		q.Notes[q.Current] = e.Draft
		q.Current = (q.Current + e.Delta + n) % n

		return []Effect{EffSetDraft{Text: q.Notes[q.Current]}}, true
	case QuestionAnswer:
		return q.answer(e.Draft), true
	case QuestionDismiss:
		return s.interrupt(), true
	default:
		return nil, false
	}

	return nil, true
}

// answer answers the shown question with note, then shows the next
// unanswered one, or sends the answers once every question has one.
func (q *Questions) answer(note string) []Effect {
	q.Notes[q.Current], q.Done[q.Current] = strings.TrimSpace(note), true
	for i := range len(q.Questions) {
		next := (q.Current + 1 + i) % len(q.Questions)
		if !q.Done[next] {
			q.Current = next

			return []Effect{EffSetDraft{Text: q.Notes[next]}}
		}
	}
	q.Sent = true

	return []Effect{EffSetDraft{}, EffAnswerQuestions{ID: q.ID, Answers: q.Answers()}}
}

// Answers are the answers in Codex's encoding: the chosen option's label,
// or "None of the above", then the note as "user_note: …".
func (q *Questions) Answers() engine.Answers {
	out := engine.Answers{}
	for i, question := range q.Questions {
		if !q.Done[i] {
			out[question.ID] = engine.Answer{Answers: []string{}}

			continue
		}
		answers := []string{ChoiceLabel(question, q.Choice[i])}
		if q.Notes[i] != "" {
			answers = append(answers, engine.NotePrefix+q.Notes[i])
		}
		out[question.ID] = engine.Answer{Answers: answers}
	}

	return out
}

// ChoiceLabel is a row's label: an option's, or "None of the above".
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
// label · note".
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
